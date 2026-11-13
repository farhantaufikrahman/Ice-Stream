import { createContext } from 'react'
import type { RegisterInput } from '../api/endpoints'
import type { Profile } from '../api/types'

/**
 * - loading: sedang mengecek cookie refresh token saat halaman dibuka.
 * - authenticated: sudah login, user berisi profil.
 * - anonymous: belum login.
 */
export type AuthStatus = 'loading' | 'authenticated' | 'anonymous'

export interface AuthState {
  status: AuthStatus
  user: Profile | null
  login: (loginName: string, password: string) => Promise<void>
  register: (input: RegisterInput) => Promise<void>
  /** Akhir login Google/GitHub: menukar kode sekali pakai dengan sesi. */
  loginWithCode: (code: string) => Promise<void>
  logout: () => Promise<void>
  /** Mengganti profil yang tersimpan, misalnya setelah nama atau foto diubah. */
  updateUser: (profile: Profile) => void
}

export const AuthContext = createContext<AuthState | null>(null)
