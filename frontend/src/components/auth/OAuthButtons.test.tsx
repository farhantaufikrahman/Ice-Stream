import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { OAuthProvider } from '../../api/types'
import { OAuthButtons } from './OAuthButtons'

const listOAuthProviders = vi.fn<() => Promise<OAuthProvider[]>>()
vi.mock('../../api/endpoints', () => ({ listOAuthProviders: () => listOAuthProviders() }))

describe('OAuthButtons', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('hanya menampilkan penyedia yang dinyalakan server', async () => {
    listOAuthProviders.mockResolvedValue([{ id: 'github', start_url: 'http://localhost:4000/api/v1/auth/oauth/github/start' }])
    const assign = vi.fn()
    vi.stubGlobal('location', { ...window.location, assign })
    render(<OAuthButtons next="/studio?tab=1" />)

    fireEvent.click(await screen.findByRole('button', { name: 'GitHub' }))
    expect(screen.queryByRole('button', { name: 'Google' })).not.toBeInTheDocument()
    expect(assign).toHaveBeenCalledWith('http://localhost:4000/api/v1/auth/oauth/github/start?next=%2Fstudio%3Ftab%3D1')
  })

  it('tidak menampilkan apa pun jika tidak ada penyedia', async () => {
    listOAuthProviders.mockResolvedValue([])
    const { container } = render(<OAuthButtons next="/" />)
    await Promise.resolve()
    expect(container).toBeEmptyDOMElement()
  })
})
