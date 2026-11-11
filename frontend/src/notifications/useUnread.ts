import { useContext } from 'react'
import { UnreadContext, type UnreadState } from './context'

/** Membaca jumlah notifikasi belum dibaca dari UnreadProvider terdekat. */
export function useUnread(): UnreadState {
  const ctx = useContext(UnreadContext)
  if (!ctx) {
    throw new Error('useUnread harus dipakai di dalam <UnreadProvider>')
  }
  return ctx
}
