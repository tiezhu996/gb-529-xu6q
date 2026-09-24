import { request, requestPage } from './client'
import type { FreezeInput, FreezeStatus, PeriodFreeze } from '../types/freeze'

interface FreezeQuery {
  tankId?: number
  status?: FreezeStatus
}

function queryString(query: FreezeQuery): string {
  const params = new URLSearchParams({ page: '1', page_size: '100' })
  if (query.tankId) params.set('tank_id', String(query.tankId))
  if (query.status) params.set('status', query.status)
  return params.toString()
}

export const listFreezes = (query: FreezeQuery = {}) =>
  requestPage<PeriodFreeze>(`/freezes?${queryString(query)}`)
export const getFreeze = (id: number) => request<PeriodFreeze>(`/freezes/${id}`)
export const createFreeze = (input: FreezeInput) =>
  request<PeriodFreeze>('/freezes', { method: 'POST', body: JSON.stringify(input) })
export const reviewFreeze = (id: number, version: number, targetStatus: Extract<FreezeStatus, 'active' | 'rejected'>, reviewNote = '') =>
  request<PeriodFreeze>(`/freezes/${id}/review`, {
    method: 'POST',
    body: JSON.stringify({ version, target_status: targetStatus, review_note: reviewNote })
  })
export const releaseFreeze = (id: number, version: number, releaseReason: string) =>
  request<PeriodFreeze>(`/freezes/${id}/release`, {
    method: 'POST',
    body: JSON.stringify({ version, release_reason: releaseReason })
  })
