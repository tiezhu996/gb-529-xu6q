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
	tank, err := s.tankRepo.Get(ctx, request.TankID)
	if err != nil {
		return model.PeriodFreeze{}, err
	}
	if request.PeriodStart == nil || request.PeriodEnd == nil {
		return model.PeriodFreeze{}, api.NewError(400, "FREEZE_PERIOD_REQUIRED", "必须提供冻结期间起止时间")
	}
	start, end := request.PeriodStart.UTC(), request.PeriodEnd.UTC()
	if !end.After(start) {
		return model.PeriodFreeze{}, api.NewError(422, "INVALID_FREEZE_PERIOD", "冻结结束时间必须晚于开始时间")
	}
	if end.Sub(start) > 90*24*time.Hour {
		return model.PeriodFreeze{}, api.NewError(422, "FREEZE_PERIOD_TOO_LONG", "单次储罐期间冻结不能超过 90 天")
	}
	note := strings.TrimSpace(request.FreezeNote)
	item := model.PeriodFreeze{
		TankID:       tank.ID,
		PeriodStart:  start,
		PeriodEnd:    end,
		FreezeNote:   note,
		FreezeStatus: string(constants.FreezePendingReview),
		Version:      1,
		CreatedBy:    actor.UserID,
	}
	if err := s.repo.Create(ctx, &item, actor); err != nil {
		return model.PeriodFreeze{}, err
	}
	return s.repo.Get(ctx, item.ID)
}

func (s *FreezeService) Review(ctx context.Context, id uint, request dto.ReviewFreezeRequest, actor repository.Actor) (model.PeriodFreeze, error) {
	if !constants.CanReview(actor.Role) {
		return model.PeriodFreeze{}, api.ErrForbidden
	}
	target := constants.FreezeStatus(request.TargetStatus)
	note := strings.TrimSpace(request.ReviewNote)
	if target == constants.FreezeRejected && len(note) < 6 {
		return model.PeriodFreeze{}, api.NewError(422, "FREEZE_REVIEW_NOTE_REQUIRED", "驳回冻结申请时必须填写不少于 6 个字符的复核说明")
	}
	return s.repo.Review(ctx, id, request.Version, target, note, actor.UserID, actor)
}

func (s *FreezeService) Release(ctx context.Context, id uint, request dto.ReleaseFreezeRequest, actor repository.Actor) (model.PeriodFreeze, error) {
	if !constants.CanReview(actor.Role) {
		return model.PeriodFreeze{}, api.ErrForbidden
	}
	reason := strings.TrimSpace(request.ReleaseReason)
	if len(reason) < 6 {
		return model.PeriodFreeze{}, api.NewError(422, "FREEZE_RELEASE_REASON_REQUIRED", "解除冻结必须填写不少于 6 个字符的原因")
	}
	return s.repo.Release(ctx, id, request.Version, reason, actor.UserID, actor)
}
