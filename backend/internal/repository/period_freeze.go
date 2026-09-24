package repository

import (
	"context"
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
	if err := query.
		Preload("Tank").Preload("Creator").Preload("Reviewer").Preload("Releaser").
		Order("period_start DESC, id DESC").
		Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("list period freezes: %w", err)
	}
	return items, total, nil
}

func (r *FreezeRepository) Get(ctx context.Context, id uint) (model.PeriodFreeze, error) {
	var item model.PeriodFreeze
	if err := r.db.WithContext(ctx).
		Preload("Tank").Preload("Creator").Preload("Reviewer").Preload("Releaser").
		First(&item, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return model.PeriodFreeze{}, api.NewError(404, "FREEZE_NOT_FOUND", "期间冻结记录不存在")
		}
		return model.PeriodFreeze{}, fmt.Errorf("get period freeze: %w", err)
	}
	return item, nil
}

// freezeWindowOverlap 与转移重叠判定一致，使用半开区间：start < other.end AND end > other.start。
func freezeWindowOverlap(query *gorm.DB, tankID uint, start, end time.Time) *gorm.DB {
	return query.Where("tank_id = ? AND period_start < ? AND period_end > ?", tankID, end.UTC(), start.UTC())
}

func (r *FreezeRepository) Create(ctx context.Context, item *model.PeriodFreeze, actor Actor) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var overlaps int64
		if err := freezeWindowOverlap(tx.Model(&model.PeriodFreeze{}), item.TankID, item.PeriodStart, item.PeriodEnd).
			Where("freeze_status IN ?", []string{string(constants.FreezePendingReview), string(constants.FreezeActive)}).
			Count(&overlaps).Error; err != nil {
			return fmt.Errorf("check freeze period overlap: %w", err)
		}
		if overlaps > 0 {
			return api.NewError(409, "FREEZE_PERIOD_OVERLAP", "该储罐已存在时间重叠且未终结的期间冻结记录")
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

func (r *FreezeRepository) Review(ctx context.Context, id, version uint, target constants.FreezeStatus, note string, reviewerID uint, actor Actor) (model.PeriodFreeze, error) {
	var updated model.PeriodFreeze
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var before model.PeriodFreeze
		if err := tx.First(&before, id).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return api.NewError(404, "FREEZE_NOT_FOUND", "期间冻结记录不存在")
			}
			return fmt.Errorf("load period freeze for review: %w", err)
		}
		if before.Version != version {
			return api.NewError(409, "FREEZE_VERSION_CONFLICT", "冻结记录版本已变化，请刷新后重试")
		}
		if !constants.CanReviewFreeze(constants.FreezeStatus(before.FreezeStatus), target) {
			return api.WithDetails(api.NewError(409, "INVALID_FREEZE_TRANSITION", "当前冻结状态不允许该复核决定"), map[string]any{
				"current": before.FreezeStatus, "target": target,
			})
		}
		if target == constants.FreezeActive {
			conflict, found, err := activeFreezeOverlap(tx, before.TankID, before.PeriodStart, before.PeriodEnd)
			if err != nil {
				return err
			}
			if found {
				return api.WithDetails(api.NewError(409, "FREEZE_PERIOD_OVERLAP", "该储罐已有生效冻结覆盖重叠期间，不能重复通过"), map[string]any{
					"freeze_id": conflict.ID,
				})
			}
		}
		now := time.Now().UTC()
		result := tx.Model(&model.PeriodFreeze{}).
			Where("id = ? AND version = ? AND freeze_status = ?", id, version, before.FreezeStatus).
			Updates(map[string]any{
				"freeze_status": target,
				"version":       gorm.Expr("version + 1"),
				"review_note":   note,
				"reviewed_by":   reviewerID,
				"reviewed_at":   now,
			})
		if result.Error != nil {
			return fmt.Errorf("review period freeze: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return api.NewError(409, "FREEZE_VERSION_CONFLICT", "冻结记录被其他请求更新")
		}
		if err := tx.First(&updated, id).Error; err != nil {
			return fmt.Errorf("reload period freeze: %w", err)
		}
		audit := NewAudit(actor, "period_freeze."+string(target), "period_freeze", id, before, updated)
		if err := tx.Create(&audit).Error; err != nil {
			return fmt.Errorf("audit period freeze review: %w", err)
		}
		return nil
	})
	return updated, err
}

func (r *FreezeRepository) Release(ctx context.Context, id, version uint, reason string, releaserID uint, actor Actor) (model.PeriodFreeze, error) {
	var updated model.PeriodFreeze
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var before model.PeriodFreeze
		if err := tx.First(&before, id).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return api.NewError(404, "FREEZE_NOT_FOUND", "期间冻结记录不存在")
			}
			return fmt.Errorf("load period freeze for release: %w", err)
		}
		if before.Version != version {
			return api.NewError(409, "FREEZE_VERSION_CONFLICT", "冻结记录版本已变化，请刷新后重试")
		}
		if !constants.CanReleaseFreeze(constants.FreezeStatus(before.FreezeStatus), constants.FreezeReleased) {
			return api.WithDetails(api.NewError(409, "INVALID_FREEZE_TRANSITION", "只有生效中的冻结记录可以解除"), map[string]any{
				"current": before.FreezeStatus,
			})
		}
		now := time.Now().UTC()
		result := tx.Model(&model.PeriodFreeze{}).
			Where("id = ? AND version = ? AND freeze_status = ?", id, version, before.FreezeStatus).
			Updates(map[string]any{
				"freeze_status":  constants.FreezeReleased,
				"version":        gorm.Expr("version + 1"),
				"release_reason": reason,
				"released_by":    releaserID,
				"released_at":    now,
			})
		if result.Error != nil {
			return fmt.Errorf("release period freeze: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return api.NewError(409, "FREEZE_VERSION_CONFLICT", "冻结记录被其他请求更新")
		}
		if err := tx.First(&updated, id).Error; err != nil {
			return fmt.Errorf("reload period freeze: %w", err)
		}
		audit := NewAudit(actor, "period_freeze.released", "period_freeze", id, before, updated)
		if err := tx.Create(&audit).Error; err != nil {
			return fmt.Errorf("audit period freeze release: %w", err)
		}
		return nil
	})
	return updated, err
}

// activeFreezeOverlap 在给定事务内查询与该冻结窗口半开相交的其它生效冻结，
// 用于复核通过前的最终检查；相邻期间端点相接不算重叠。
func activeFreezeOverlap(tx *gorm.DB, tankID uint, start, end time.Time) (model.PeriodFreeze, bool, error) {
	var freeze model.PeriodFreeze
	result := tx.
		Where("tank_id = ? AND freeze_status = ? AND period_start < ? AND period_end > ?", tankID, string(constants.FreezeActive), end.UTC(), start.UTC()).
		Order("period_start ASC, id ASC").First(&freeze)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return model.PeriodFreeze{}, false, nil
		}
		return model.PeriodFreeze{}, false, fmt.Errorf("check active freeze range: %w", result.Error)
	}
	return freeze, true, nil
}

// ActiveFreezeAtPoint 在给定事务内查询覆盖某一时间点（含边界）的生效冻结。
func ActiveFreezeAtPoint(tx *gorm.DB, tankID uint, point time.Time) (model.PeriodFreeze, bool, error) {
	var freeze model.PeriodFreeze
	utc := point.UTC()
	result := tx.
		Where("tank_id = ? AND freeze_status = ? AND period_start <= ? AND period_end >= ?", tankID, string(constants.FreezeActive), utc, utc).
		Order("period_start ASC, id ASC").First(&freeze)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return model.PeriodFreeze{}, false, nil
		}
		return model.PeriodFreeze{}, false, fmt.Errorf("check active freeze point: %w", result.Error)
	}
	return freeze, true, nil
}

// ActiveFreezeOverlapRange 在给定事务内查询与时间段相交（含边界接触）的生效冻结。
func ActiveFreezeOverlapRange(tx *gorm.DB, tankID uint, start, end time.Time) (model.PeriodFreeze, bool, error) {
	var freeze model.PeriodFreeze
	result := tx.
		Where("tank_id = ? AND freeze_status = ? AND period_start <= ? AND period_end >= ?", tankID, string(constants.FreezeActive), end.UTC(), start.UTC()).
		Order("period_start ASC, id ASC").First(&freeze)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return model.PeriodFreeze{}, false, nil
		}
		return model.PeriodFreeze{}, false, fmt.Errorf("check active freeze range: %w", result.Error)
	}
	return freeze, true, nil
}

// FreezeBlockedError 构造带冻结记录编号的统一阻断错误，回报给现场操作者。
func FreezeBlockedError(action string, freeze model.PeriodFreeze) *api.Error {
	messages := map[string]string{
		"measurement_create": "该时间点处于已冻结储罐期间，期内不能新增计量快照",
		"transfer_create":    "该时间段处于已冻结储罐期间，期内不能新增物理转移",
		"transfer_status":    "该物理转移处于储罐冻结期间内，冻结期间不能确认或取消转移",
	}
	message := messages[action]
	if message == "" {
		message = "该储罐期间已冻结，当前变更被阻止"
	}
	return api.WithDetails(api.NewError(409, "PERIOD_FROZEN", fmt.Sprintf("%s（冻结记录 #%d）", message, freeze.ID)), map[string]any{
		"freeze_id":     freeze.ID,
		"freeze_status": freeze.FreezeStatus,
	})
}
