package model

import "time"

// PeriodFreeze 冻结储罐的一个计量期间：待复核通过后，期内的计量快照补录、
// 物理转移新增及确认/取消都会被挡下。冻结不删除任何既有数据，只阻止新增变更。
type PeriodFreeze struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	TankID      uint      `json:"tank_id" gorm:"not null;index:idx_freeze_period,priority:1"`
	PeriodStart time.Time `json:"period_start" gorm:"not null;index:idx_freeze_period,priority:2"`
	PeriodEnd   time.Time `json:"period_end" gorm:"not null;index:idx_freeze_period,priority:3"`
	FreezeNote  string    `json:"freeze_note" gorm:"size:500;not null"`
	// status: pending_approval -> active -> released；复核驳回则为 rejected
	FreezeStatus string `json:"freeze_status" gorm:"type:varchar(24);not null;index;check:freeze_status IN ('pending_approval','active','rejected','released')"`

	CreatedBy uint      `json:"created_by" gorm:"not null"`
	CreatedAt time.Time `json:"created_at"`

	DecidedBy    *uint      `json:"decided_by"`
	DecidedAt    *time.Time `json:"decided_at"`
	DecisionNote string     `json:"decision_note" gorm:"size:500"`

	ReleasedBy    *uint      `json:"released_by"`
	ReleasedAt    *time.Time `json:"released_at"`
	ReleaseReason string     `json:"release_reason" gorm:"size:500"`

	Version uint `json:"version" gorm:"not null;default:1"`

	Tank *StorageTank `json:"tank,omitempty" gorm:"foreignKey:TankID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT"`
}

func (PeriodFreeze) TableName() string { return "period_freezes" }
