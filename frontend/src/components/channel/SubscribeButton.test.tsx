import { act, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../../api/client'
import type { SubscriptionResult } from '../../api/types'
import type { AuthStatus } from '../../auth/context'
import { SubscribeButton } from './SubscribeButton'

// Ganti API dan status login dengan versi palsu, supaya tes tidak butuh server.
const setSubscription = vi.fn<(username: string, on: boolean) => Promise<SubscriptionResult>>()
vi.mock('../../api/endpoints', () => ({ setSubscription: (u: string, on: boolean) => setSubscription(u, on) }))
let authStatus: AuthStatus = 'authenticated'
vi.mock('../../auth/useAuth', () => ({ useAuth: () => ({ status: authStatus }) }))

function renderButton(props: Partial<Parameters<typeof SubscribeButton>[0]> = {}) {
  const onChange = vi.fn()
  render(
    <MemoryRouter initialEntries={['/channel/budi']}>
      <SubscribeButton username="budi" subscribed={false} subscriberCount={10} isOwn={false} onChange={onChange} {...props} />
    </MemoryRouter>,
  )
  return onChange
}

describe('SubscribeButton', () => {
  beforeEach(() => {
    authStatus = 'authenticated'
    setSubscription.mockReset()
  })

  it('tamu diarahkan ke halaman Masuk lalu kembali ke kanal', () => {
    authStatus = 'anonymous'
    renderButton()
    expect(screen.getByRole('link', { name: 'Ikuti' })).toHaveAttribute('href', '/login?next=%2Fchannel%2Fbudi')
  })

  it('kanal sendiri menampilkan Kelola video', () => {
    renderButton({ isOwn: true })
    expect(screen.getByRole('link', { name: 'Kelola video' })).toHaveAttribute('href', '/studio/content')
  })

  it('langsung berubah (optimistic), lalu memakai jawaban server', async () => {
    setSubscription.mockResolvedValue({ subscribed: true, subscriber_count: 12 })
    const onChange = renderButton()
    await act(async () => fireEvent.click(screen.getByRole('button', { name: 'Ikuti' })))
    expect(setSubscription).toHaveBeenCalledWith('budi', true)
    expect(onChange).toHaveBeenNthCalledWith(1, { subscribed: true, subscriber_count: 11 })
    expect(onChange).toHaveBeenNthCalledWith(2, { subscribed: true, subscriber_count: 12 })
  })

  it('jika gagal, kembali seperti semula dan menampilkan pesan', async () => {
    setSubscription.mockRejectedValue(new ApiError(400, 'INVALID_ARGUMENT', 'tidak bisa subscribe channel sendiri'))
    const onChange = renderButton({ subscribed: true })
    await act(async () => fireEvent.click(screen.getByRole('button', { name: 'Mengikuti' })))
    expect(setSubscription).toHaveBeenCalledWith('budi', false)
    expect(onChange).toHaveBeenLastCalledWith({ subscribed: true, subscriber_count: 10 })
    expect(screen.getByRole('alert')).toHaveTextContent('tidak bisa subscribe channel sendiri')
  })
})
