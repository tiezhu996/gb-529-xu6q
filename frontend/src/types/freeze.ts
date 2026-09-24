import type { StorageTank } from './tank'
import type { User } from './auth'

export type FreezeStatus = 'pending_review' | 'active' | 'rejected' | 'released'

export interface PeriodFreeze {
  id: number
  tank_id: number
  period_start: string
  period_end: string
  freeze_note: string
  freeze_status: FreezeStatus
  version: number
  created_by: number
  reviewed_by: number | null
  review_note: string
  reviewed_at: string | null
  released_by: number | null
  release_reason: string
  released_at: string | null
  created_at: string
  updated_at: string
  tank?: StorageTank
  creator?: User
  reviewer?: User
  releaser?: User
}

export interface FreezeInput {
  tank_id: number
  period_start: string
  period_end: string
  freeze_note: string
}
