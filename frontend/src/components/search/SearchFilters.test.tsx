import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { SearchFilters } from './SearchFilters'

describe('SearchFilters', () => {
  it('menandai pilihan aktif dengan aria-pressed', () => {
    render(<SearchFilters sort="views" duration="short" onSort={vi.fn()} onDuration={vi.fn()} />)
    expect(screen.getByRole('button', { name: 'Paling banyak ditonton' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'Paling relevan' })).toHaveAttribute('aria-pressed', 'false')
    expect(screen.getByRole('button', { name: '< 10 menit' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('memilih urutan dan durasi, dan melepas durasi yang aktif', () => {
    const onSort = vi.fn()
    const onDuration = vi.fn()
    render(<SearchFilters sort="relevance" duration="medium" onSort={onSort} onDuration={onDuration} />)
    fireEvent.click(screen.getByRole('button', { name: 'Danmaku terbanyak' }))
    expect(onSort).toHaveBeenCalledWith('danmaku')
    fireEvent.click(screen.getByRole('button', { name: '> 30 menit' }))
    expect(onDuration).toHaveBeenLastCalledWith('long')
    fireEvent.click(screen.getByRole('button', { name: '10–30 menit' }))
    expect(onDuration).toHaveBeenLastCalledWith('')
  })
})
