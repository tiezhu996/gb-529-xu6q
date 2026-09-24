package dto

import "time"

type CreateFreezeRequest struct {
	TankID      uint       `json:"tank_id" binding:"required"`
	PeriodStart *time.Time `json:"period_start" binding:"required"`
	PeriodEnd   *time.Time `json:"period_end" binding:"required"`
	FreezeNote  string     `json:"freeze_note" binding:"required,min=3,max=500"`
}

type DecideFreezeRequest struct {
	Version      uint   `json:"version" binding:"required"`
	DecisionNote string `json:"decision_note" binding:"max=500"`
}

type ReleaseFreezeRequest struct {
	Version       uint   `json:"version" binding:"required"`
	ReleaseReason string `json:"release_reason" binding:"required,min=6,max=500"`
}
