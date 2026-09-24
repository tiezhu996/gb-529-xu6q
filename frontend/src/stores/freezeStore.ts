import { create } from 'zustand'
import * as api from '../api/freezes'
import type { FreezeInput, FreezeStatus, PeriodFreeze } from '../types/freeze'

interface FreezeState {
  items: PeriodFreeze[]
  loading: boolean
  load: (tankId?: number, status?: FreezeStatus) => Promise<void>
  create: (input: FreezeInput) => Promise<PeriodFreeze>
  approve: (item: PeriodFreeze, decisionNote?: string) => Promise<void>
  reject: (item: PeriodFreeze, decisionNote: string) => Promise<void>
  release: (item: PeriodFreeze, releaseReason: string) => Promise<void>
}

function replace(items: PeriodFreeze[], updated: PeriodFreeze): PeriodFreeze[] {
  return items.map((candidate) => candidate.id === updated.id ? updated : candidate)
}

export const useFreezeStore = create<FreezeState>((set, get) => ({
  items: [],
  loading: false,
  load: async (tankId, status) => {
    set({ loading: true })
    try {
      const result = await api.listFreezes(tankId, status)
      set({ items: result.items })
    } finally {
      set({ loading: false })
    }
  },
  create: async (input) => {
    const created = await api.createFreeze(input)
    set((state) => ({ items: [created, ...state.items] }))
    return created
  },
  approve: async (item, decisionNote) => {
    const updated = await api.decideFreeze(item.id, item.version, 'approve', decisionNote)
    set({ items: replace(get().items, updated) })
  },
  reject: async (item, decisionNote) => {
    const updated = await api.decideFreeze(item.id, item.version, 'reject', decisionNote)
    set({ items: replace(get().items, updated) })
  },
  release: async (item, releaseReason) => {
    const updated = await api.releaseFreeze(item.id, item.version, releaseReason)
    set({ items: replace(get().items, updated) })
  }
}))
