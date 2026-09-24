package service

import (
	"context"
	"strings"
	"time"

	"lng-boiloff-gas-balance/backend/internal/constants"
	"lng-boiloff-gas-balance/backend/internal/dto"
	"lng-boiloff-gas-balance/backend/internal/model"
	"lng-boiloff-gas-balance/backend/internal/repository"
	"lng-boiloff-gas-balance/backend/pkg/api"
)

const maxFreezePeriod = 90 * 24 * time.Hour

type FreezeService struct {
	repo     *repository.FreezeRepository
	tankRepo *repository.TankRepository
}

func NewFreezeService(repo *repository.FreezeRepository, tankRepo *repository.TankRepository) *FreezeService {
	return &FreezeService{repo: repo, tankRepo: tankRepo}
}

func (s *FreezeService) List(ctx context.Context, filter repository.FreezeFilter) ([]model.PeriodFreeze, int64, error) {
	if filter.Status != "" && !constants.ValidFreezeStatus(constants.FreezeStatus(filter.Status)) {
		return nil, 0, api.NewError(400, "INVALID_FREEZE_STATUS", "冻结状态筛选值无效")
	}
	if filter.From != nil && filter.To != nil && !filter.To.After(*filter.From) {
		return nil, 0, api.NewError(400, "INVALID_TIME_RANGE", "结束时间必须晚于开始时间")
	}
	return s.repo.List(ctx, filter)
}

func (s *FreezeService) Get(ctx context.Context, id uint) (model.PeriodFreeze, error) {
	return s.repo.Get(ctx, id)
}

func (s *FreezeService) Create(ctx context.Context, request dto.CreateFreezeRequest, actor repository.Actor) (model.PeriodFreeze, error) {
	if !constants.CanAnalyze(actor.Role) {
		return model.PeriodFreeze{}, api.ErrForbidden
	}
	if request.PeriodStart == nil || request.PeriodEnd == nil {
		return model.PeriodFreeze{}, api.NewError(400, "FREEZE_PERIOD_REQUIRED", "必须提供冻结期间起止时间")
	}
	start, end := request.PeriodStart.UTC(), request.PeriodEnd.UTC()
	if !end.After(start) {
		return model.PeriodFreeze{}, api.NewError(422, "INVALID_FREEZE_PERIOD", "冻结期间结束时间必须晚于开始时间")
	}
	if end.Sub(start) > maxFreezePeriod {
		return model.PeriodFreeze{}, api.NewError(422, "FREEZE_PERIOD_TOO_LONG", "单次冻结期间不能超过 90 天")
	}
	tank, err := s.tankRepo.Get(ctx, request.TankID)
	if err != nil {
		return model.PeriodFreeze{}, err
	}
	item := model.PeriodFreeze{
		TankID:       tank.ID,
		PeriodStart:  start,
		PeriodEnd:    end,
		FreezeNote:   strings.TrimSpace(request.FreezeNote),
		FreezeStatus: string(constants.FreezePendingApproval),
		CreatedBy:    actor.UserID,
		Version:      1,
	}
	if err := s.repo.Create(ctx, &item, actor); err != nil {
		return model.PeriodFreeze{}, err
	}
	item.Tank = &tank
	return item, nil
}

func (s *FreezeService) Approve(ctx context.Context, id uint, request dto.DecideFreezeRequest, actor repository.Actor) (model.PeriodFreeze, error) {
	if !constants.CanReview(actor.Role) {
		return model.PeriodFreeze{}, api.ErrForbidden
	}
	return s.repo.Decide(ctx, id, request.Version, true, strings.TrimSpace(request.DecisionNote), actor)
}

func (s *FreezeService) Reject(ctx context.Context, id uint, request dto.DecideFreezeRequest, actor repository.Actor) (model.PeriodFreeze, error) {
	if !constants.CanReview(actor.Role) {
		return model.PeriodFreeze{}, api.ErrForbidden
	}
	note := strings.TrimSpace(request.DecisionNote)
	if len(note) < 6 {
		return model.PeriodFreeze{}, api.NewError(422, "REJECTION_REASON_REQUIRED", "驳回冻结申请时必须填写不少于 6 个字符的说明")
	}
	return s.repo.Decide(ctx, id, request.Version, false, note, actor)
}

func (s *FreezeService) Release(ctx context.Context, id uint, request dto.ReleaseFreezeRequest, actor repository.Actor) (model.PeriodFreeze, error) {
	if !constants.CanReview(actor.Role) {
		return model.PeriodFreeze{}, api.ErrForbidden
	}
	reason := strings.TrimSpace(request.ReleaseReason)
	if len(reason) < 6 {
		return model.PeriodFreeze{}, api.NewError(422, "RELEASE_REASON_REQUIRED", "解除冻结必须填写不少于 6 个字符的原因")
	}
	return s.repo.Release(ctx, id, request.Version, reason, actor)
}
