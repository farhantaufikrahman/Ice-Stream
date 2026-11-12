import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ShareDialog } from './ShareDialog'

function renderDialog(currentSeconds: number) {
  render(<ShareDialog videoId="42" title="Bikin gelato" currentSeconds={currentSeconds} onClose={vi.fn()} />)
  return screen.getByLabelText('Tautan') as HTMLInputElement
}

describe('ShareDialog', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('tautan tanpa dan dengan waktu mulai', () => {
    const input = renderDialog(83.6)
    expect(input.value).toBe(`${window.location.origin}/watch/42`)
    fireEvent.click(screen.getByRole('checkbox', { name: 'Mulai di 1:23' }))
    expect(input.value).toBe(`${window.location.origin}/watch/42?t=83`)
  })

  it('pilihan waktu mulai tidak ada jika video belum diputar', () => {
    renderDialog(0)
    expect(screen.queryByRole('checkbox')).not.toBeInTheDocument()
  })

  it('menyalin tautan, atau memilih teksnya jika clipboard ditolak', async () => {
    const writeText = vi.fn<(text: string) => Promise<void>>().mockResolvedValue()
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    renderDialog(10)
    await act(async () => fireEvent.click(screen.getByRole('button', { name: 'Salin' })))
    expect(writeText).toHaveBeenCalledWith(`${window.location.origin}/watch/42`)
    expect(screen.getByText('Tautan sudah disalin.')).toBeInTheDocument()

    writeText.mockRejectedValue(new Error('ditolak'))
    await act(async () => fireEvent.click(screen.getByRole('button', { name: 'Tersalin' })))
    expect(screen.getByText(/tekan Ctrl\+C/)).toBeInTheDocument()
  })
})
