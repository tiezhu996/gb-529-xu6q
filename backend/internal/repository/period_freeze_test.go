package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"lng-boiloff-gas-balance/backend/internal/constants"
	"lng-boiloff-gas-balance/backend/internal/model"
	"lng-boiloff-gas-balance/backend/pkg/api"
)

func newFreezeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.AutoMigrate(
		&model.StorageTank{}, &model.MeasurementSnapshot{},
		&model.TransferOperation{}, &model.PeriodFreeze{}, &model.AuditEvent{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	tank := model.StorageTank{
		TankCode: "TK-FZ", Name: "冻结测试罐", NominalCapacityM3: 180000,
		MinLevelM: 0, MaxLevelM: 12, ReferenceDensityKGM3: 452, ReferenceTemperatureC: -160,
		ThermalExpansionPerC: 0.0035, CapacityCurveJSON: datatypes.JSON([]byte(`[]`)),
		CoefficientVersion: "CV-T", TankStatus: "active", Version: 1,
	}
	if err := db.Create(&tank).Error; err != nil {
		t.Fatalf("create tank: %v", err)
	}
	return db
}

var freezeActor = Actor{UserID: 7, Email: "analyst@lng.local", Role: constants.RoleProcessAnalyst, RequestID: "req-test"}
var reviewerActor = Actor{UserID: 8, Email: "reviewer@lng.local", Role: constants.RoleReviewer, RequestID: "req-review"}

func appErrorCode(t *testing.T, err error) *api.Error {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	appErr, ok := err.(*api.Error)
	if !ok {
		t.Fatalf("expected *api.Error, got %T: %v", err, err)
	}
	return appErr
}

func TestPendingFreezeOverlapRules(t *testing.T) {
	ctx := context.Background()
	db := newFreezeTestDB(t)
	freezes := NewFreezeRepository(db)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	first := &model.PeriodFreeze{TankID: 1, PeriodStart: start, PeriodEnd: start.Add(24 * time.Hour),
		FreezeNote: "平衡已送审，冻结期间", FreezeStatus: string(constants.FreezePendingApproval), CreatedBy: 7, Version: 1}
	if err := freezes.Create(ctx, first, freezeActor); err != nil {
		t.Fatalf("create first freeze: %v", err)
	}

	overlap := &model.PeriodFreeze{TankID: 1, PeriodStart: start.Add(12 * time.Hour), PeriodEnd: start.Add(36 * time.Hour),
		FreezeNote: "重叠期间", FreezeStatus: string(constants.FreezePendingApproval), CreatedBy: 7, Version: 1}
	if code := appErrorCode(t, freezes.Create(ctx, overlap, freezeActor)).Code; code != "FREEZE_PERIOD_OVERLAP" {
		t.Fatalf("overlap code = %s, want FREEZE_PERIOD_OVERLAP", code)
	}

	adjacent := &model.PeriodFreeze{TankID: 1, PeriodStart: start.Add(24 * time.Hour), PeriodEnd: start.Add(48 * time.Hour),
		FreezeNote: "端点相接", FreezeStatus: string(constants.FreezePendingApproval), CreatedBy: 7, Version: 1}
	if err := freezes.Create(ctx, adjacent, freezeActor); err != nil {
		t.Fatalf("adjacent freeze should be allowed: %v", err)
	}
}

func TestActiveFreezeBlocksSnapshotTransfersAndReportsID(t *testing.T) {
	ctx := context.Background()
	db := newFreezeTestDB(t)
	freezeRepo := NewFreezeRepository(db)
	snapshotRepo := NewMeasurementRepository(db)
	transferRepo := NewTransferRepository(db)

	start := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	freeze := &model.PeriodFreeze{TankID: 1, PeriodStart: start, PeriodEnd: start.Add(24 * time.Hour),
		FreezeNote: "冻结", FreezeStatus: string(constants.FreezePendingApproval), CreatedBy: 7, Version: 1}
	if err := freezeRepo.Create(ctx, freeze, freezeActor); err != nil {
		t.Fatalf("create freeze: %v", err)
	}

	// 待复核期间允许预先存在一条草稿转移（后续验证它不能在生效期内确认/取消）
	draft := &model.TransferOperation{
		TankID: 1, OperationType: "outflow", StartAt: start.Add(2 * time.Hour), EndAt: start.Add(3 * time.Hour),
		MeasuredMassKG: 50000, MeasurementUncertaintyPct: 0.3, CounterpartyRef: "METER-DRAFT",
		OperationStatus: "draft", Version: 1, CreatedBy: 7,
	}
	if err := transferRepo.Create(ctx, draft, freezeActor); err != nil {
		t.Fatalf("create draft transfer while pending: %v", err)
	}

	approved, err := freezeRepo.Decide(ctx, freeze.ID, 1, true, "同意冻结", reviewerActor)
	if err != nil {
		t.Fatalf("approve freeze: %v", err)
	}
	if approved.FreezeStatus != string(constants.FreezeActive) || approved.Version != 2 {
		t.Fatalf("unexpected approved freeze: %+v", approved)
	}

	// 期内补录快照被挡下，并回报冻结记录编号
	insideSnapshot := &model.MeasurementSnapshot{
		TankID: 1, MeasuredAt: start.Add(5 * time.Hour), LiquidLevelM: 8, LiquidTempC: -160,
		VaporPressureKPA: 110, DensityKGM3: 451, CalculatedVolumeM3: 1, TemperatureDensityKGM3: 451,
		CalculatedLiquidMassKG: 1, MeasurementUncertaintyPct: 0.3,
		QualityFlag: constants.QualityGood, SourceNote: "期内补录", CreatedBy: 7,
	}
	blocked := appErrorCode(t, snapshotRepo.Create(ctx, insideSnapshot, freezeActor))
	if blocked.Code != "PERIOD_FROZEN" || blocked.HTTPStatus != 423 {
		t.Fatalf("snapshot block = %s/%d, want PERIOD_FROZEN/423", blocked.Code, blocked.HTTPStatus)
	}
	if blocked.Details["freeze_id"] != freeze.ID {
		t.Fatalf("freeze_id detail = %v, want %d", blocked.Details["freeze_id"], freeze.ID)
	}

	// 边界点同样受冻结保护
	boundary := *insideSnapshot
	boundary.MeasuredAt = start
	if err := snapshotRepo.Create(ctx, &boundary, freezeActor); err == nil {
		t.Fatal("snapshot at freeze boundary must be blocked")
	}

	// 期外快照不受影响
	outside := *insideSnapshot
	outside.MeasuredAt = start.Add(48 * time.Hour)
	outside.SourceNote = "期外补录"
	if err := snapshotRepo.Create(ctx, &outside, freezeActor); err != nil {
		t.Fatalf("snapshot outside frozen period should pass: %v", err)
	}

	// 与冻结期相交的新增转移被挡下
	insideTransfer := &model.TransferOperation{
		TankID: 1, OperationType: "inflow", StartAt: start.Add(6 * time.Hour), EndAt: start.Add(7 * time.Hour),
		MeasuredMassKG: 10000, MeasurementUncertaintyPct: 0.3, CounterpartyRef: "METER-INSIDE",
		OperationStatus: "draft", Version: 1, CreatedBy: 7,
	}
	if code := appErrorCode(t, transferRepo.Create(ctx, insideTransfer, freezeActor)).Code; code != "PERIOD_FROZEN" {
		t.Fatalf("transfer create code = %s, want PERIOD_FROZEN", code)
	}

	// 端点相接的期外转移允许新增
	adjacentTransfer := &model.TransferOperation{
		TankID: 1, OperationType: "inflow", StartAt: start.Add(25 * time.Hour), EndAt: start.Add(26 * time.Hour),
		MeasuredMassKG: 10000, MeasurementUncertaintyPct: 0.3, CounterpartyRef: "METER-ADJACENT",
		OperationStatus: "draft", Version: 1, CreatedBy: 7,
	}
	if err := transferRepo.Create(ctx, adjacentTransfer, freezeActor); err != nil {
		t.Fatalf("adjacent transfer should be allowed: %v", err)
	}

	// 期内既有转移的确认与取消都被挡下
	if _, err := transferRepo.Transition(ctx, draft.ID, 1, "confirmed", "", freezeActor); err == nil {
		t.Fatal("confirming transfer inside frozen period must be blocked")
	} else if appErr := appErrorCode(t, err); appErr.Code != "PERIOD_FROZEN" {
		t.Fatalf("confirm code = %s, want PERIOD_FROZEN", appErr.Code)
	}
	if _, err := transferRepo.Transition(ctx, draft.ID, 1, "cancelled", "取消原因不少于六个字", freezeActor); err == nil {
		t.Fatal("cancelling transfer inside frozen period must be blocked")
	}

	// 解除冻结后，期内操作恢复
	released, err := freezeRepo.Release(ctx, freeze.ID, 2, "补录已核对，解除冻结", reviewerActor)
	if err != nil {
		t.Fatalf("release freeze: %v", err)
	}
	if released.FreezeStatus != string(constants.FreezeReleased) || released.ReleasedBy == nil {
		t.Fatalf("unexpected released freeze: %+v", released)
	}
	if _, err := transferRepo.Transition(ctx, draft.ID, 1, "confirmed", "", freezeActor); err != nil {
		t.Fatalf("confirm after release should pass: %v", err)
	}
}

func TestFreezeDecisionGuards(t *testing.T) {
	ctx := context.Background()
	db := newFreezeTestDB(t)
	freezeRepo := NewFreezeRepository(db)
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	freeze := &model.PeriodFreeze{TankID: 1, PeriodStart: start, PeriodEnd: start.Add(12 * time.Hour),
		FreezeNote: "冻结", FreezeStatus: string(constants.FreezePendingApproval), CreatedBy: 7, Version: 1}
	if err := freezeRepo.Create(ctx, freeze, freezeActor); err != nil {
		t.Fatalf("create freeze: %v", err)
	}

	// 版本不符
	if _, err := freezeRepo.Decide(ctx, freeze.ID, 99, true, "", reviewerActor); err == nil {
		t.Fatal("stale version must be rejected")
	}

	rejected, err := freezeRepo.Decide(ctx, freeze.ID, 1, false, "依据不足予以驳回", reviewerActor)
	if err != nil {
		t.Fatalf("reject freeze: %v", err)
	}
	if rejected.FreezeStatus != string(constants.FreezeRejected) {
		t.Fatalf("status = %s, want rejected", rejected.FreezeStatus)
	}

	// 已驳回不能再次决定，也不能解除
	if _, err := freezeRepo.Decide(ctx, freeze.ID, 2, true, "", reviewerActor); err == nil {
		t.Fatal("deciding a rejected freeze must fail")
	}
	if _, err := freezeRepo.Release(ctx, freeze.ID, 2, "尝试解除驳回记录", reviewerActor); err == nil {
		t.Fatal("releasing a rejected freeze must fail")
	}

	// 驳回记录不再阻挡新的冻结登记
	replacement := &model.PeriodFreeze{TankID: 1, PeriodStart: start, PeriodEnd: start.Add(12 * time.Hour),
		FreezeNote: "重新申请", FreezeStatus: string(constants.FreezePendingApproval), CreatedBy: 7, Version: 1}
	if err := freezeRepo.Create(ctx, replacement, freezeActor); err != nil {
		t.Fatalf("replacement freeze after rejection should pass: %v", err)
	}
}
