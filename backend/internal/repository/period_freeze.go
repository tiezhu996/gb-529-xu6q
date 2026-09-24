package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"lng-boiloff-gas-balance/backend/internal/constants"
	"lng-boiloff-gas-balance/backend/internal/model"
	"lng-boiloff-gas-balance/backend/pkg/api"
)

type FreezeFilter struct {
	TankID   uint
	Status   string
	From     *time.Time
	To       *time.Time
	Page     int
	PageSize int
}

type FreezeRepository struct {
	db *gorm.DB
}

func NewFreezeRepository(db *gorm.DB) *FreezeRepository { return &FreezeRepository{db: db} }

// blockingFreezeError 以冻结记录编号构造 423 业务错误。
func blockingFreezeError(freeze model.PeriodFreeze, action string) *api.Error {
	return api.WithDetails(api.NewError(httpStatusLocked, "PERIOD_FROZEN",
		"该储罐期间已冻结，相关变更被挡下，请凭冻结记录编号联系复核员或管理员"), map[string]any{
		"freeze_id": freeze.ID,
		"tank_id":   freeze.TankID,
		"action":    action,
	})
}

const httpStatusLocked = 423

// ErrNoBlockingFreeze 表示没有命中任何生效冻结。
var ErrNoBlockingFreeze = errors.New("no blocking period freeze")

// FindBlocking 在给定 DB（可处于事务内）上查找覆盖时间点 instant 的生效冻结。
// 边界采用闭区间，与平衡期间取数口径保持一致。
func FindBlocking(ctx context.Context, db *gorm.DB, tankID uint, instant time.Time) (model.PeriodFreeze, error) {
	var freeze model.PeriodFreeze
	err := db.WithContext(ctx).
		Where("tank_id = ? AND freeze_status = ? AND period_start <= ? AND period_end >= ?",
			tankID, constants.FreezeActive, instant.UTC(), instant.UTC()).
		Order("period_start DESC, id DESC").First(&freeze).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.PeriodFreeze{}, ErrNoBlockingFreeze
	}
	if err != nil {
		return model.PeriodFreeze{}, fmt.Errorf("find blocking period freeze: %w", err)
	}
	return freeze, nil
}

// FindBlockingRange 查找与 [start, end] 区间相交的生效冻结（端点相接不算相交）。
func FindBlockingRange(ctx context.Context, db *gorm.DB, tankID uint, start, end time.Time) (model.PeriodFreeze, error) {
	var freeze model.PeriodFreeze
	err := db.WithContext(ctx).
		Where("tank_id = ? AND freeze_status = ? AND period_start < ? AND period_end > ?",
			tankID, constants.FreezeActive, end.UTC(), start.UTC()).
		Order("period_start DESC, id DESC").First(&freeze).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.PeriodFreeze{}, ErrNoBlockingFreeze
	}
	if err != nil {
		return model.PeriodFreeze{}, fmt.Errorf("find blocking period freeze range: %w", err)
	}
	return freeze, nil
}

// ensureNotFrozen 在事务内确认时间点没有生效冻结，否则返回带冻结编号的 423 错误。
func ensureNotFrozen(tx *gorm.DB, action string, tankID uint, instant time.Time) error {
	freeze, err := FindBlocking(tx.Statement.Context, tx, tankID, instant)
	if errors.Is(err, ErrNoBlockingFreeze) {
		return nil
	}
	if err != nil {
		return err
	}
	return blockingFreezeError(freeze, action)
}

// ensureRangeNotFrozen 在事务内确认时间区间没有与生效冻结相交。
func ensureRangeNotFrozen(tx *gorm.DB, action string, tankID uint, start, end time.Time) error {
	freeze, err := FindBlockingRange(tx.Statement.Context, tx, tankID, start, end)
	if errors.Is(err, ErrNoBlockingFreeze) {
		return nil
	}
	if err != nil {
		return err
	}
	return blockingFreezeError(freeze, action)
}

func (r *FreezeRepository) List(ctx context.Context, filter FreezeFilter) ([]model.PeriodFreeze, int64, error) {
	filter.Page, filter.PageSize = normalizePage(filter.Page, filter.PageSize)
	query := r.db.WithContext(ctx).Model(&model.PeriodFreeze{})
	if filter.TankID > 0 {
		query = query.Where("tank_id = ?", filter.TankID)
	}
	if filter.Status != "" {
		query = query.Where("freeze_status = ?", filter.Status)
	}
	if filter.From != nil {
		query = query.Where("period_end >= ?", filter.From.UTC())
	}
	if filter.To != nil {
		query = query.Where("period_start <= ?", filter.To.UTC())
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count period freezes: %w", err)
	}
	var items []model.PeriodFreeze
	if err := query.Preload("Tank").Order("created_at DESC, id DESC").
		Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("list period freezes: %w", err)
	}
	return items, total, nil
}

func (r *FreezeRepository) Get(ctx context.Context, id uint) (model.PeriodFreeze, error) {
	var item model.PeriodFreeze
	if err := r.db.WithContext(ctx).Preload("Tank").First(&item, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.PeriodFreeze{}, api.NewError(404, "FREEZE_NOT_FOUND", "冻结记录不存在")
		}
		return model.PeriodFreeze{}, fmt.Errorf("get period freeze: %w", err)
	}
	return item, nil
}

// Create 登记一条待复核冻结；同一储罐待复核/生效冻结期间不得重叠（端点可相接）。
func (r *FreezeRepository) Create(ctx context.Context, item *model.PeriodFreeze, actor Actor) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var overlaps int64
		if err := tx.Model(&model.PeriodFreeze{}).
			Where("tank_id = ? AND freeze_status IN ? AND period_start < ? AND period_end > ?",
				item.TankID, []constants.FreezeStatus{constants.FreezePendingApproval, constants.FreezeActive},
				item.PeriodEnd, item.PeriodStart).
			Count(&overlaps).Error; err != nil {
			return fmt.Errorf("check freeze period overlap: %w", err)
		}
		if overlaps > 0 {
			return api.NewError(409, "FREEZE_PERIOD_OVERLAP", "该储罐已有重叠的待复核或生效冻结期间")
		}
		if err := tx.Create(item).Error; err != nil {
			return fmt.Errorf("create period freeze: %w", err)
		}
		audit := NewAudit(actor, "period_freeze.created", "period_freeze", item.ID, nil, item)
		if err := tx.Create(&audit).Error; err != nil {
			return fmt.Errorf("audit period freeze creation: %w", err)
		}
		return nil
	})
}

// Decide 复核员通过或驳回复核中的冻结；通过时再次校验期间重叠。
func (r *FreezeRepository) Decide(ctx context.Context, id, version uint, approve bool, note string, actor Actor) (model.PeriodFreeze, error) {
	var updated model.PeriodFreeze
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var before model.PeriodFreeze
		if err := tx.First(&before, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return api.NewError(404, "FREEZE_NOT_FOUND", "冻结记录不存在")
			}
			return fmt.Errorf("load period freeze: %w", err)
		}
		if before.FreezeStatus != string(constants.FreezePendingApproval) {
			return api.WithDetails(api.NewError(409, "FREEZE_NOT_PENDING", "只有待复核冻结可以做出通过或驳回决定"), map[string]any{
				"current": before.FreezeStatus,
			})
		}
		if before.Version != version {
			return api.NewError(409, "FREEZE_VERSION_CONFLICT", "冻结记录版本已变化，请刷新后重试")
		}
		target := constants.FreezeRejected
		if approve {
			target = constants.FreezeActive
			var overlaps int64
			if err := tx.Model(&model.PeriodFreeze{}).
				Where("id <> ? AND tank_id = ? AND freeze_status = ? AND period_start < ? AND period_end > ?",
					before.ID, before.TankID, constants.FreezeActive, before.PeriodEnd, before.PeriodStart).
				Count(&overlaps).Error; err != nil {
				return fmt.Errorf("recheck freeze period overlap: %w", err)
			}
			if overlaps > 0 {
				return api.NewError(409, "FREEZE_PERIOD_OVERLAP", "该储罐已有生效冻结期间重叠，无法通过")
			}
		}
		now := time.Now().UTC()
		result := tx.Model(&model.PeriodFreeze{}).
			Where("id = ? AND version = ? AND freeze_status = ?", id, version, constants.FreezePendingApproval).
			Updates(map[string]any{
				"freeze_status": target,
				"version":       gorm.Expr("version + 1"),
				"decided_by":    actor.UserID,
				"decided_at":    now,
				"decision_note": note,
			})
		if result.Error != nil {
			return fmt.Errorf("decide period freeze: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return api.NewError(409, "FREEZE_VERSION_CONFLICT", "冻结记录被其他请求更新")
		}
		if err := tx.First(&updated, id).Error; err != nil {
			return fmt.Errorf("reload period freeze: %w", err)
		}
		audit := NewAudit(actor, "period_freeze."+string(target), "period_freeze", id, before, map[string]any{
			"record": updated, "note": note,
		})
		if err := tx.Create(&audit).Error; err != nil {
			return fmt.Errorf("audit period freeze decision: %w", err)
		}
		return nil
	})
	return updated, err
}

// Release 带原因解除生效冻结。
func (r *FreezeRepository) Release(ctx context.Context, id, version uint, reason string, actor Actor) (model.PeriodFreeze, error) {
	var updated model.PeriodFreeze
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var before model.PeriodFreeze
		if err := tx.First(&before, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return api.NewError(404, "FREEZE_NOT_FOUND", "冻结记录不存在")
			}
			return fmt.Errorf("load period freeze for release: %w", err)
		}
		if before.FreezeStatus != string(constants.FreezeActive) {
			return api.WithDetails(api.NewError(409, "FREEZE_NOT_ACTIVE", "只有生效冻结可以解除"), map[string]any{
				"current": before.FreezeStatus,
			})
		}
		if before.Version != version {
			return api.NewError(409, "FREEZE_VERSION_CONFLICT", "冻结记录版本已变化，请刷新后重试")
		}
		now := time.Now().UTC()
		result := tx.Model(&model.PeriodFreeze{}).
			Where("id = ? AND version = ? AND freeze_status = ?", id, version, constants.FreezeActive).
			Updates(map[string]any{
				"freeze_status":  constants.FreezeReleased,
				"version":        gorm.Expr("version + 1"),
				"released_by":    actor.UserID,
				"released_at":    now,
				"release_reason": reason,
			})
		if result.Error != nil {
			return fmt.Errorf("release period freeze: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return api.NewError(409, "FREEZE_VERSION_CONFLICT", "冻结记录被其他请求更新")
		}
		if err := tx.First(&updated, id).Error; err != nil {
			return fmt.Errorf("reload released period freeze: %w", err)
		}
		audit := NewAudit(actor, "period_freeze.released", "period_freeze", id, before, map[string]any{
			"record": updated, "reason": reason,
		})
		if err := tx.Create(&audit).Error; err != nil {
			return fmt.Errorf("audit period freeze release: %w", err)
		}
		return nil
	})
	return updated, err
}
