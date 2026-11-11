import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { onSessionLost, refreshAccessToken } from '../api/client'
import * as api from '../api/endpoints'
import type { Profile } from '../api/types'
import { AuthContext, type AuthState, type AuthStatus } from './context'

// Menyimpan siapa yang sedang login dan membagikannya ke semua komponen
// lewat React Context. Mirip req.user di Express, tapi di sisi browser.
export function AuthProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<AuthStatus>('loading')
  const [user, setUser] = useState<Profile | null>(null)

  useEffect(() => {
    // StrictMode di mode development menjalankan effect dua kali. Aman,
    // karena refreshAccessToken() memakai satu request yang sama selama
    // request pertama belum selesai.
    let active = true
    const restore = async () => {
      try {
        const token = await refreshAccessToken()
        const profile = token ? await api.getMyProfile() : null
        if (!active) return
        setUser(profile)
        setStatus(profile ? 'authenticated' : 'anonymous')
      } catch {
        // Server mati atau error lain: tampilkan sebagai belum login.
        if (!active) return
        setUser(null)
        setStatus('anonymous')
      }
    }
    void restore()
    const stop = onSessionLost(() => {
      setUser(null)
      setStatus('anonymous')
    })
    return () => {
      active = false
      stop()
    }
  }, [])

  const login = useCallback(async (loginName: string, password: string) => {
    await api.login(loginName, password)
    const profile = await api.getMyProfile()
    setUser(profile)
    setStatus('authenticated')
  }, [])

  const register = useCallback(
    async (input: api.RegisterInput) => {
      await api.register(input)
      await login(input.username, input.password)
    },
    [login],
  )

  const logout = useCallback(async () => {
    try {
      await api.logout()
    } finally {
      setUser(null)
      setStatus('anonymous')
    }
  }, [])

  // Hanya dipakai saat sudah login: tamu tidak punya profil untuk diganti.
  const updateUser = useCallback((profile: Profile) => {
    // Email hanya ada di beberapa jawaban server: jika kosong, pakai yang lama.
    setUser((current) => (current && current.id === profile.id ? { ...profile, email: profile.email ?? current.email } : current))
  }, [])

  const value = useMemo<AuthState>(
    () => ({ status, user, login, register, logout, updateUser }),
    [status, user, login, register, logout, updateUser],
  )
  return <AuthContext value={value}>{children}</AuthContext>
}
