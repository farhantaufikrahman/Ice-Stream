import { describe, expect, it } from 'vitest'
import { avatarFileError, bioError, displayNameError, MAX_AVATAR_BYTES } from './profile'

describe('displayNameError', () => {
  it('wajib diisi (spasi saja dianggap kosong)', () => {
    expect(displayNameError('   ')).toBe('Nama tampilan wajib diisi.')
  })
  it('menghitung emoji sebagai 1 karakter, seperti server', () => {
    expect(displayNameError('🍦'.repeat(50))).toBeNull()
    expect(displayNameError('a'.repeat(51))).toBe('Maksimal 50 karakter.')
  })
})

describe('bioError', () => {
  it('boleh kosong, maksimal 500 karakter', () => {
    expect(bioError('')).toBeNull()
    expect(bioError('x'.repeat(500))).toBeNull()
    expect(bioError('x'.repeat(501))).toBe('Maksimal 500 karakter.')
  })
})

describe('avatarFileError', () => {
  it('menerima JPEG, PNG, dan WebP sampai 2 MB', () => {
    expect(avatarFileError({ type: 'image/png', size: 1000 })).toBeNull()
    expect(avatarFileError({ type: 'image/webp', size: MAX_AVATAR_BYTES })).toBeNull()
  })
  it('menolak format lain, file kosong, dan file terlalu besar', () => {
    expect(avatarFileError({ type: 'image/gif', size: 1000 })).toMatch(/Format tidak didukung/)
    expect(avatarFileError({ type: 'image/jpeg', size: 0 })).toBe('File kosong.')
    expect(avatarFileError({ type: 'image/jpeg', size: MAX_AVATAR_BYTES + 1 })).toBe('Ukuran maksimal 2 MB.')
  })
})
