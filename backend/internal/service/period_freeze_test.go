package service

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"lng-boiloff-gas-balance/backend/internal/balance"
	"lng-boiloff-gas-balance/backend/internal/constants"
	"lng-boiloff-gas-balance/backend/internal/dto"
	"lng-boiloff-gas-balance/backend/internal/model"
	"lng-boiloff-gas-balance/backend/internal/repository"
	"lng-boiloff-gas-balance/backend/pkg/api"
)

var freezeFixtureSeq uint64

type freezeFixture struct {
	db        *gorm.DB
	analyst   repository.Actor
	reviewer  repository.Actor
	admin     repository.Actor
	tank      model.StorageTank
	freezeSvc *FreezeService
	measSvc   *MeasurementService
	transfer  *TransferService
	measRepo  *repository.MeasurementRepository
	transRepo *repository.TransferRepository
}

func newFreezeFixture(t *testing.T) freezeFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:period-freeze-"+strconv.FormatUint(atomic.AddUint64(&freezeFixtureSeq, 1), 10)+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.StorageTank{}, &model.MeasurementSnapshot{},
		&model.TransferOperation{}, &model.BalanceRun{}, &model.PeriodFreeze{}, &model.AuditEvent{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	users := []model.User{
		{Email: "freeze-analyst@lng.local", DisplayName: "分析员", PasswordHash: "x", Role: constants.RoleProcessAnalyst, Active: true},
		{Email: "freeze-reviewer@lng.local", DisplayName: "复核员", PasswordHash: "x", Role: constants.RoleReviewer, Active: true},
		{Email: "freeze-admin@lng.local", DisplayName: "管理员", PasswordHash: "x", Role: constants.RoleAdmin, Active: true},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}
	raw, _ := balance.NewCapacityCurve([]float64{0, 15000})
	curve, _ := raw.Marshal()
	tank := model.StorageTank{
		TankCode: "TK-FREEZE", Name: "冻结验证储罐", NominalCapacityM3: 180000, MinLevelM: 0, MaxLevelM: 12,
		ReferenceDensityKGM3: 452, ReferenceTemperatureC: -160, ThermalExpansionPerC: 0.0035,
		CapacityCurveJSON: datatypes.JSON(curve), CoefficientVersion: "CV-F1", TankStatus: "active", Version: 1,
	}
	if err := db.Create(&tank).Error; err != nil {
		t.Fatalf("seed tank: %v", err)
	}
	tankRepo := repository.NewTankRepository(db)
	measRepo := repository.NewMeasurementRepository(db)
	transRepo := repository.NewTransferRepository(db)
	freezeRepo := repository.NewFreezeRepository(db)
	return freezeFixture{
		db:        db,
		analyst:   repository.Actor{UserID: users[0].ID, Email: users[0].Email, Role: users[0].Role, RequestID: "req-analyst"},
		reviewer:  repository.Actor{UserID: users[1].ID, Email: users[1].Email, Role: users[1].Role, RequestID: "req-reviewer"},
		admin:     repository.Actor{UserID: users[2].ID, Email: users[2].Email, Role: users[2].Role, RequestID: "req-admin"},
		tank:      tank,
		freezeSvc: NewFreezeService(freezeRepo, tankRepo),
		measSvc:   NewMeasurementService(measRepo, tankRepo),
		transfer:  NewTransferService(transRepo, tankRepo),
		measRepo:  measRepo,
		transRepo: transRepo,
	}
}

func (f freezeFixture) snapshotAt(t *testing.T, at time.Time, level float64) model.MeasurementSnapshot {
	t.Helper()
	created, err := f.measSvc.Create(context.Background(), dto.CreateMeasurementRequest{
		TankID: f.tank.ID, MeasuredAt: &at, LiquidLevelM: level, LiquidTempC: -160,
		VaporPressureKPA: 112, DensityKGM3: 451, MeasurementUncertaintyPct: 0.3,
		QualityFlag: "good", SourceNote: "冻结流程验证快照",
	}, f.analyst)
	if err != nil {
		t.Fatalf("create snapshot at %s: %v", at.Format(time.RFC3339), err)
	}
	return created
}

func assertErrorCode(t *testing.T, err error, code string) *api.Error {
	t.Helper()
	var appErr *api.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected api error %s, got %v", code, err)
	}
	if appErr.Code != code {
		t.Fatalf("error code = %s, want %s", appErr.Code, code)
	}
	return appErr
}

func TestFreezeLifecycleBlocksFrozenPeriodAndRestoresAfterRelease(t *testing.T) {
	f := newFreezeFixture(t)
	ctx := context.Background()
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	// 冻结前的既有快照可正常补录。
	f.snapshotAt(t, start.Add(6*time.Hour), 10.0)

	freeze, err := f.freezeSvc.Create(ctx, dto.CreateFreezeRequest{
		TankID: f.tank.ID, PeriodStart: &start, PeriodEnd: &end, FreezeNote: "平衡结果送审后锁定该储罐期间",
	}, f.analyst)
	if err != nil {
		t.Fatalf("create freeze: %v", err)
	}
	if freeze.FreezeStatus != string(constants.FreezePendingReview) {
		t.Fatalf("new freeze status = %s, want pending_review", freeze.FreezeStatus)
	}

	// 复核通过前（待复核）期间内仍允许补录，复核通过后才锁定。
	f.snapshotAt(t, start.Add(7*time.Hour), 9.9)

	// 待复核阶段先登记一条期内草稿转移；冻结通过后它的确认与取消都要被挡下。
	frozenDraft, err := f.transfer.Create(ctx, dto.CreateTransferRequest{
		TankID: f.tank.ID, OperationType: "inflow",
		StartAt: ptrTime(start.Add(4 * time.Hour)), EndAt: ptrTime(start.Add(5 * time.Hour)),
		MeasuredMassKG: 18000, MeasurementUncertaintyPct: 0.25,
		CounterpartyRef: "FROZEN-DRAFT-01", OperationStatus: "draft",
	}, f.analyst)
	if err != nil {
		t.Fatalf("seed pending-window draft: %v", err)
	}

	active, err := f.freezeSvc.Review(ctx, freeze.ID, dto.ReviewFreezeRequest{
		TargetStatus: "active", Version: freeze.Version, ReviewNote: "证据一致，通过冻结",
	}, f.reviewer)
	if err != nil {
		t.Fatalf("approve freeze: %v", err)
	}
	if active.FreezeStatus != string(constants.FreezeActive) || active.ReviewedBy == nil {
		t.Fatalf("freeze not active with reviewer: %+v", active)
	}

	// 期内新增快照被挡下，并回报冻结记录编号。
	_, err = f.measSvc.Create(ctx, dto.CreateMeasurementRequest{
		TankID: f.tank.ID, MeasuredAt: ptrTime(start.Add(8 * time.Hour)), LiquidLevelM: 9.8, LiquidTempC: -160,
		VaporPressureKPA: 112, DensityKGM3: 451, MeasurementUncertaintyPct: 0.3,
		QualityFlag: "good", SourceNote: "冻结期内补录应被阻止",
	}, f.analyst)
	blocked := assertErrorCode(t, err, "PERIOD_FROZEN")
	if blocked.Details["freeze_id"] != freeze.ID {
		t.Fatalf("block details = %v, want freeze_id %d", blocked.Details, freeze.ID)
	}

	// 期间端点（期末时刻）同样被视为冻结期内。
	_, err = f.measSvc.Create(ctx, dto.CreateMeasurementRequest{
		TankID: f.tank.ID, MeasuredAt: ptrTime(end), LiquidLevelM: 9.8, LiquidTempC: -160,
		VaporPressureKPA: 112, DensityKGM3: 451, MeasurementUncertaintyPct: 0.3,
		QualityFlag: "good", SourceNote: "期末边界补录应被阻止",
	}, f.analyst)
	assertErrorCode(t, err, "PERIOD_FROZEN")

	// 冻结期之外的快照仍可写入。
	f.snapshotAt(t, end.Add(time.Hour), 9.7)

	// 期内新增转移被挡下。
	_, err = f.transfer.Create(ctx, dto.CreateTransferRequest{
		TankID: f.tank.ID, OperationType: "outflow",
		StartAt: ptrTime(start.Add(2 * time.Hour)), EndAt: ptrTime(start.Add(3 * time.Hour)),
		MeasuredMassKG: 12000, MeasurementUncertaintyPct: 0.25,
		CounterpartyRef: "FROZEN-NEW-01", OperationStatus: "draft",
	}, f.analyst)
	assertErrorCode(t, err, "PERIOD_FROZEN")

	// 冻结通过前已存在的期内草稿转移，确认与取消都要被挡下。
	_, err = f.transfer.Transition(ctx, frozenDraft.ID, dto.TransitionTransferRequest{
		TargetStatus: "confirmed", Version: frozenDraft.Version,
	}, f.analyst)
	assertErrorCode(t, err, "PERIOD_FROZEN")
	_, err = f.transfer.Transition(ctx, frozenDraft.ID, dto.TransitionTransferRequest{
		TargetStatus: "cancelled", Version: frozenDraft.Version, Reason: "冻结期内尝试取消",
	}, f.analyst)
	assertErrorCode(t, err, "PERIOD_FROZEN")

	// 期外草稿转移的确认不受影响。
	outsideDraft := mustTransfer(t, f, start.Add(-3*time.Hour), start.Add(-2*time.Hour), "OUTSIDE-DRAFT-01")
	confirmed, err := f.transfer.Transition(ctx, outsideDraft.ID, dto.TransitionTransferRequest{
		TargetStatus: "confirmed", Version: outsideDraft.Version,
	}, f.analyst)
	if err != nil {
		t.Fatalf("confirm outside-window transfer: %v", err)
	}
	if confirmed.OperationStatus != "confirmed" {
		t.Fatalf("outside transfer status = %s", confirmed.OperationStatus)
	}

	// 既有快照与转移仍可查询（数据保持可查）。
	snapshots, err := f.measRepo.ListForTank(ctx, f.tank.ID)
	if err != nil {
		t.Fatalf("list snapshots while frozen: %v", err)
	}
	if len(snapshots) != 3 {
		t.Fatalf("expected existing snapshots to remain readable, got %d", len(snapshots))
	}
	_, total, err := f.transRepo.List(ctx, repository.TransferFilter{TankID: f.tank.ID})
	if err != nil || total != 2 {
		t.Fatalf("expected transfers readable while frozen, total=%d err=%v", total, err)
	}

	// 解除冻结必须带原因；原因不足会被拒绝。
	_, err = f.freezeSvc.Release(ctx, freeze.ID, dto.ReleaseFreezeRequest{
		Version: active.Version, ReleaseReason: "无",
	}, f.admin)
	assertErrorCode(t, err, "FREEZE_RELEASE_REASON_REQUIRED")

	// 解除后期内补录恢复。
	released, err := f.freezeSvc.Release(ctx, freeze.ID, dto.ReleaseFreezeRequest{
		Version: active.Version, ReleaseReason: "现场完成数据更正申请，审批解除冻结",
	}, f.admin)
	if err != nil {
		t.Fatalf("admin release freeze: %v", err)
	}
	if released.FreezeStatus != string(constants.FreezeReleased) || released.ReleasedBy == nil || released.ReleaseReason == "" {
		t.Fatalf("release record incomplete: %+v", released)
	}
	if f.snapshotAt(t, start.Add(9*time.Hour), 9.6).ID == 0 {
		t.Fatal("snapshot inside released window should be accepted")
	}
}

func TestFreezePeriodCannotOverlapAcrossLifecycle(t *testing.T) {
	f := newFreezeFixture(t)
	ctx := context.Background()
	first := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	firstEnd := first.Add(24 * time.Hour)
	one, err := f.freezeSvc.Create(ctx, dto.CreateFreezeRequest{
		TankID: f.tank.ID, PeriodStart: &first, PeriodEnd: &firstEnd, FreezeNote: "第一段冻结期间",
	}, f.analyst)
	if err != nil {
		t.Fatalf("create first freeze: %v", err)
	}
	overlapStart := first.Add(12 * time.Hour)
	overlapEnd := first.Add(36 * time.Hour)
	_, err = f.freezeSvc.Create(ctx, dto.CreateFreezeRequest{
		TankID: f.tank.ID, PeriodStart: &overlapStart, PeriodEnd: &overlapEnd, FreezeNote: "与待复核期间重叠应拒绝",
	}, f.analyst)
	assertErrorCode(t, err, "FREEZE_PERIOD_OVERLAP")

	// 相邻（端点相接、不重叠）允许登记。
	adjacent := firstEnd
	adjacentEnd := firstEnd.Add(24 * time.Hour)
	two, err := f.freezeSvc.Create(ctx, dto.CreateFreezeRequest{
		TankID: f.tank.ID, PeriodStart: &adjacent, PeriodEnd: &adjacentEnd, FreezeNote: "与前一期间首尾相接",
	}, f.analyst)
	if err != nil {
		t.Fatalf("adjacent freeze should be allowed: %v", err)
	}
	if _, err := f.freezeSvc.Review(ctx, one.ID, dto.ReviewFreezeRequest{
		TargetStatus: "active", Version: one.Version,
	}, f.reviewer); err != nil {
		t.Fatalf("activate first freeze: %v", err)
	}
	if _, err := f.freezeSvc.Review(ctx, two.ID, dto.ReviewFreezeRequest{
		TargetStatus: "active", Version: two.Version,
	}, f.reviewer); err != nil {
		t.Fatalf("adjacent freeze activation should pass: %v", err)
	}
}

func TestFreezeReviewRequiresNoteAndRespectsRoles(t *testing.T) {
	f := newFreezeFixture(t)
	ctx := context.Background()
	start := time.Date(2026, 5, 2, 8, 0, 0, 0, time.UTC)
	end := start.Add(12 * time.Hour)
	freeze, err := f.freezeSvc.Create(ctx, dto.CreateFreezeRequest{
		TankID: f.tank.ID, PeriodStart: &start, PeriodEnd: &end, FreezeNote: "角色与驳回说明验证",
	}, f.analyst)
	if err != nil {
		t.Fatalf("create freeze: %v", err)
	}

	// 复核员不能登记冻结，分析员不能复核。
	_, err = f.freezeSvc.Create(ctx, dto.CreateFreezeRequest{
		TankID: f.tank.ID, PeriodStart: ptrTime(end.Add(48 * time.Hour)), PeriodEnd: ptrTime(end.Add(60 * time.Hour)), FreezeNote: "复核员越权登记",
	}, f.reviewer)
	assertErrorCode(t, err, "ACCESS_DENIED")
	_, err = f.freezeSvc.Review(ctx, freeze.ID, dto.ReviewFreezeRequest{
		TargetStatus: "active", Version: freeze.Version,
	}, f.analyst)
	assertErrorCode(t, err, "ACCESS_DENIED")

	// 驳回必须填写说明；驳回后该期间不产生阻断。
	_, err = f.freezeSvc.Review(ctx, freeze.ID, dto.ReviewFreezeRequest{
		TargetStatus: "rejected", Version: freeze.Version,
	}, f.reviewer)
	assertErrorCode(t, err, "FREEZE_REVIEW_NOTE_REQUIRED")
	rejected, err := f.freezeSvc.Review(ctx, freeze.ID, dto.ReviewFreezeRequest{
		TargetStatus: "rejected", Version: freeze.Version, ReviewNote: "期间边界快照不足，驳回冻结申请",
	}, f.reviewer)
	if err != nil {
		t.Fatalf("reject freeze: %v", err)
	}
	if rejected.FreezeStatus != string(constants.FreezeRejected) {
		t.Fatalf("freeze status = %s, want rejected", rejected.FreezeStatus)
	}
	if f.snapshotAt(t, start.Add(time.Hour), 8.8).ID == 0 {
		t.Fatal("rejected freeze must not block snapshot creation")
	}

	// 已驳回记录不能解除。
	_, err = f.freezeSvc.Release(ctx, rejected.ID, dto.ReleaseFreezeRequest{
		Version: rejected.Version, ReleaseReason: "尝试解除被驳回记录",
	}, f.admin)
	assertErrorCode(t, err, "INVALID_FREEZE_TRANSITION")
}

func TestFreezeStateTransitions(t *testing.T) {
	pending := constants.FreezePendingReview
	if !constants.CanReviewFreeze(pending, constants.FreezeActive) ||
		!constants.CanReviewFreeze(pending, constants.FreezeRejected) {
		t.Fatal("pending_review freeze must allow active or rejected")
	}
	if constants.CanReviewFreeze(constants.FreezeActive, constants.FreezeActive) {
		t.Fatal("active freeze must not accept another review decision")
	}
	if !constants.CanReleaseFreeze(constants.FreezeActive, constants.FreezeReleased) {
		t.Fatal("active freeze must be releasable by reviewer or admin")
	}
	if constants.CanReleaseFreeze(constants.FreezeReleased, constants.FreezeActive) {
		t.Fatal("released freeze must remain terminal")
	}
}

func mustTransfer(t *testing.T, f freezeFixture, start, end time.Time, ref string) model.TransferOperation {
	t.Helper()
	item, err := f.transfer.Create(context.Background(), dto.CreateTransferRequest{
		TankID: f.tank.ID, OperationType: "outflow",
		StartAt: &start, EndAt: &end, MeasuredMassKG: 9000, MeasurementUncertaintyPct: 0.3,
		CounterpartyRef: ref, OperationStatus: "draft",
	}, f.analyst)
	if err != nil {
		t.Fatalf("create transfer %s: %v", ref, err)
	}
	return item
}

func ptrTime(value time.Time) *time.Time { return &value }
