import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, apiRequest, getAccessToken, onSessionLost, refreshAccessToken, setAccessToken } from './client'

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

const fetchMock = vi.fn<typeof fetch>()

beforeEach(() => {
  vi.stubGlobal('fetch', fetchMock)
  setAccessToken(null)
})

afterEach(() => {
  fetchMock.mockReset()
  vi.unstubAllGlobals()
})

function authHeader(call: number): string | undefined {
  const init = fetchMock.mock.calls[call][1]
  return (init?.headers as Record<string, string> | undefined)?.Authorization
}

describe('refreshAccessToken', () => {
  it('hanya mengirim satu request walau dipanggil bersamaan', async () => {
    fetchMock.mockResolvedValue(json(200, { access_token: 'baru', token_type: 'Bearer', expires_at: '' }))

    const results = await Promise.all([refreshAccessToken(), refreshAccessToken(), refreshAccessToken()])

    expect(results).toEqual(['baru', 'baru', 'baru'])
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ method: 'POST', credentials: 'include' })
    expect(getAccessToken()).toBe('baru')
  })

  it('mengembalikan null jika belum login (401)', async () => {
    fetchMock.mockResolvedValue(json(401, { error: { code: 'UNAUTHENTICATED', message: 'x' } }))
    await expect(refreshAccessToken()).resolves.toBeNull()
  })
})

describe('apiRequest', () => {
  it('mengulang sekali dengan token baru jika access token kedaluwarsa', async () => {
    setAccessToken('lama')
    fetchMock
      .mockResolvedValueOnce(json(401, { error: { code: 'UNAUTHENTICATED', message: 'kedaluwarsa' } }))
      .mockResolvedValueOnce(json(200, { access_token: 'baru', token_type: 'Bearer', expires_at: '' }))
      .mockResolvedValueOnce(json(200, { ok: true }))

    await expect(apiRequest('/me/profile', { auth: 'required' })).resolves.toEqual({ ok: true })

    expect(fetchMock).toHaveBeenCalledTimes(3)
    expect(authHeader(0)).toBe('Bearer lama')
    expect(authHeader(2)).toBe('Bearer baru')
  })

  it('auth optional: lanjut tanpa token jika sesi sudah berakhir', async () => {
    setAccessToken('lama')
    const lost = vi.fn()
    const stop = onSessionLost(lost)
    fetchMock
      .mockResolvedValueOnce(json(401, { error: { code: 'UNAUTHENTICATED', message: 'kedaluwarsa' } }))
      .mockResolvedValueOnce(json(401, { error: { code: 'UNAUTHENTICATED', message: 'dicabut' } }))
      .mockResolvedValueOnce(json(200, { video: { id: '1' } }))

    await expect(apiRequest('/videos/1', { auth: 'optional' })).resolves.toEqual({ video: { id: '1' } })

    expect(authHeader(2)).toBeUndefined()
    expect(lost).toHaveBeenCalledOnce()
    stop()
  })

  it('auth required tanpa sesi melempar 401 tanpa memanggil endpoint', async () => {
    fetchMock.mockResolvedValue(json(401, { error: { code: 'UNAUTHENTICATED', message: 'x' } }))

    await expect(apiRequest('/me/profile', { auth: 'required' })).rejects.toMatchObject({ status: 401 })
    expect(fetchMock).toHaveBeenCalledTimes(1) // hanya /auth/refresh
  })

  it('mengubah body error dari gateway menjadi ApiError', async () => {
    fetchMock.mockResolvedValue(json(409, { error: { code: 'ALREADY_EXISTS', message: 'username sudah dipakai' } }))

    const err = await apiRequest('/auth/register', { method: 'POST', body: {} }).catch((e: unknown) => e)

    expect(err).toBeInstanceOf(ApiError)
    expect(err).toMatchObject({ status: 409, code: 'ALREADY_EXISTS', message: 'username sudah dipakai' })
  })

  it('error yang bukan JSON tetap menjadi ApiError', async () => {
    fetchMock.mockResolvedValue(new Response('<html>bad gateway</html>', { status: 502 }))
    await expect(apiRequest('/videos')).rejects.toMatchObject({ status: 502, code: 'UNKNOWN' })
  })

  it('204 mengembalikan undefined', async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }))
    await expect(apiRequest('/auth/logout', { method: 'POST' })).resolves.toBeUndefined()
  })
})
