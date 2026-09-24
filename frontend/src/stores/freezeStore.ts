import { create } from 'zustand'
import * as api from '../api/freezes'
import type { FreezeInput, FreezeStatus, PeriodFreeze } from '../types/freeze'

interface FreezeState {
  items: PeriodFreeze[]
  loading: boolean
  statusFilter: FreezeStatus | ''
  load: (tankId?: number) => Promise<void>
  setStatusFilter: (status: FreezeStatus | '') => Promise<void>
  create: (input: FreezeInput) => Promise<PeriodFreeze>
  approve: (item: PeriodFreeze, reviewNote?: string) => Promise<void>
  reject: (item: PeriodFreeze, reviewNote: string) => Promise<void>
  release: (item: PeriodFreeze, reason: string) => Promise<void>
}

export const useFreezeStore = create<FreezeState>((set, get) => ({
  items: [],
  loading: false,
  statusFilter: '',
  load: async (tankId) => {
    set({ loading: true })
    try {
      const result = await api.listFreezes({ tankId, status: get().statusFilter || undefined })
      set({ items: result.items })
    } finally {
      set({ loading: false })
    }
  },
  setStatusFilter: async (status) => {
    set({ statusFilter: status })
    await get().load()
  },
  create: async (input) => {
    const created = await api.createFreeze(input)
    set((state) => ({ items: [created, ...state.items] }))
    return created
  },
  approve: async (item, reviewNote) => {
    const updated = await api.reviewFreeze(item.id, item.version, 'active', reviewNote)
    set((state) => ({ items: state.items.map((candidate) => candidate.id === updated.id ? updated : candidate) }))
  },
  reject: async (item, reviewNote) => {
    const updated = await api.reviewFreeze(item.id, item.version, 'rejected', reviewNote)
    set((state) => ({ items: state.items.map((candidate) => candidate.id === updated.id ? updated : candidate) }))
  },
  release: async (item, reason) => {
    const updated = await api.releaseFreeze(item.id, item.version, reason)
    set((state) => ({ items: state.items.map((candidate) => candidate.id === updated.id ? updated : candidate) }))
  }
}))
