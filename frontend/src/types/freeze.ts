import type { StorageTank } from './tank'

export type FreezeStatus = 'pending_approval' | 'active' | 'rejected' | 'released'

export interface PeriodFreeze {
  id: number
  tank_id: number
  period_start: string
  period_end: string
  freeze_note: string
  freeze_status: FreezeStatus
  created_by: number
  created_at: string
  decided_by: number | null
  decided_at: string | null
  decision_note: string
  released_by: number | null
  released_at: string | null
  release_reason: string
  version: number
  tank?: StorageTank
}

export interface FreezeInput {
  tank_id: number
  period_start: string
  period_end: string
  freeze_note: string
}

export const freezeStatusLabels: Record<FreezeStatus, string> = {
  pending_approval: '待复核',
  active: '生效中',
  rejected: '已驳回',
  released: '已解除'
}
