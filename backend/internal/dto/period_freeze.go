package dto

import "time"

type CreateFreezeRequest struct {
	TankID      uint       `json:"tank_id" binding:"required"`
	PeriodStart *time.Time `json:"period_start" binding:"required"`
	PeriodEnd   *time.Time `json:"period_end" binding:"required"`
	FreezeNote  string     `json:"freeze_note" binding:"required,min=6,max=500"`
}

type ReviewFreezeRequest struct {
	TargetStatus string `json:"target_status" binding:"required,oneof=active rejected"`
	Version      uint   `json:"version" binding:"required"`
	ReviewNote   string `json:"review_note" binding:"max=1000"`
}

type ReleaseFreezeRequest struct {
	Version       uint   `json:"version" binding:"required"`
	ReleaseReason string `json:"release_reason" binding:"required,min=6,max=1000"`
}
