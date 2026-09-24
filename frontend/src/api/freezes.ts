import { request, requestPage } from './client'
import type { FreezeInput, FreezeStatus, PeriodFreeze } from '../types/freeze'

export const listFreezes = (tankId?: number, status?: FreezeStatus) => {
  const params = new URLSearchParams({ page: '1', page_size: '100' })
  if (tankId) params.set('tank_id', String(tankId))
  if (status) params.set('status', status)
  return requestPage<PeriodFreeze>(`/period-freezes?${params.toString()}`)
}
export const getFreeze = (id: number) => request<PeriodFreeze>(`/period-freezes/${id}`)
export const createFreeze = (input: FreezeInput) =>
  request<PeriodFreeze>('/period-freezes', { method: 'POST', body: JSON.stringify(input) })
export const decideFreeze = (id: number, version: number, decision: 'approve' | 'reject', decisionNote = '') =>
  request<PeriodFreeze>(`/period-freezes/${id}/${decision}`, {
    method: 'POST',
    body: JSON.stringify({ version, decision_note: decisionNote })
  })
export const releaseFreeze = (id: number, version: number, releaseReason: string) =>
  request<PeriodFreeze>(`/period-freezes/${id}/release`, {
    method: 'POST',
    body: JSON.stringify({ version, release_reason: releaseReason })
  })
