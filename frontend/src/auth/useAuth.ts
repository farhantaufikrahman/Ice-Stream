import { useContext } from 'react'
import { AuthContext, type AuthState } from './context'

/** Membaca status login dari AuthProvider terdekat. */
export function useAuth(): AuthState {
  const ctx = useContext(AuthContext)
  if (!ctx) {
    throw new Error('useAuth harus dipakai di dalam <AuthProvider>')
  }
  return ctx
}
