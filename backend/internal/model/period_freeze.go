package model

import "time"

// PeriodFreeze 储罐期间冻结记录：复核通过后期间内的计量快照与物理转移变更被锁定，
// 但既有数据与平衡结果始终保持只读可查。
type PeriodFreeze struct {
	ID            uint         `json:"id" gorm:"primaryKey"`
	TankID        uint         `json:"tank_id" gorm:"not null;index:idx_period_freeze_window"`
	PeriodStart   time.Time    `json:"period_start" gorm:"not null;index:idx_period_freeze_window"`
	PeriodEnd     time.Time    `json:"period_end" gorm:"not null;index:idx_period_freeze_window"`
	FreezeNote    string       `json:"freeze_note" gorm:"size:500;not null"`
	FreezeStatus  string       `json:"freeze_status" gorm:"size:24;not null;index;check:freeze_status IN ('pending_review','active','rejected','released')"`
	Version       uint         `json:"version" gorm:"not null;default:1"`
	CreatedBy     uint         `json:"created_by" gorm:"not null;index"`
	ReviewedBy    *uint        `json:"reviewed_by"`
	ReviewNote    string       `json:"review_note" gorm:"size:1000;not null;default:''"`
	ReviewedAt    *time.Time   `json:"reviewed_at"`
	ReleasedBy    *uint        `json:"released_by"`
	ReleaseReason string       `json:"release_reason" gorm:"size:1000;not null;default:''"`
	ReleasedAt    *time.Time   `json:"released_at"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
	Tank          *StorageTank `json:"tank,omitempty" gorm:"foreignKey:TankID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT"`
	Creator       *User        `json:"creator,omitempty" gorm:"foreignKey:CreatedBy;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT"`
	Reviewer      *User        `json:"reviewer,omitempty" gorm:"foreignKey:ReviewedBy;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT"`
	Releaser      *User        `json:"releaser,omitempty" gorm:"foreignKey:ReleasedBy;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT"`
}

func (PeriodFreeze) TableName() string { return "period_freezes" }
