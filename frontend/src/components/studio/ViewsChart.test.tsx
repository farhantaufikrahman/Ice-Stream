import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { ViewsChart } from './ViewsChart'


describe('ViewsChart', () => {
  it('batang tertinggi amber, sumbu dibulatkan, angka tersedia untuk pembaca layar', () => {
    const daily = [3, 0, 7, 7, 1].map((views, i) => ({ date: `2026-11-0${i + 5}`, views }))
    const { container } = render(<ViewsChart daily={daily} />)

    expect(screen.getByRole('heading', { name: 'Penayangan per hari' })).toBeInTheDocument()
    expect(screen.getByText('Puncak: 7 tayangan')).toBeInTheDocument()
    // Sumbu: 8 (atas), 4 (tengah), 0.
    expect(container.querySelector('.chart-axis')?.textContent).toBe('840')
    const bars = container.querySelectorAll('.chart-bar')
    expect(bars).toHaveLength(5)
    expect(container.querySelectorAll('.chart-bar.is-peak')).toHaveLength(2)
    expect(bars[0]).toHaveAttribute('title', '5 Nov: 3 tayangan')
    expect(bars[0].getAttribute('style')).toContain('--h: 37.5%')
    // Tabel tersembunyi untuk pembaca layar.
    expect(screen.getByRole('table', { name: 'Penayangan per hari' })).toBeInTheDocument()
    expect(screen.getByRole('row', { name: '7 Nov 7' })).toBeInTheDocument()
  })

  it('tanpa tayangan: pesan kosong, tidak ada batang amber', () => {
    const daily = [0, 0, 0].map((views, i) => ({ date: `2026-11-0${i + 1}`, views }))
    const { container } = render(<ViewsChart daily={daily} />)
    expect(screen.getByText('Belum ada tayangan di periode ini.')).toBeInTheDocument()
    expect(container.querySelectorAll('.chart-bar.is-peak')).toHaveLength(0)
  })

  it('90 hari: dikelompokkan per 3 hari', () => {
    const start = new Date(2026, 7, 14)
    const daily = Array.from({ length: 90 }, (_, i) => {
      const d = new Date(start.getFullYear(), start.getMonth(), start.getDate() + i)
      return { date: `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`, views: 1 }
    })
    const { container } = render(<ViewsChart daily={daily} />)
    expect(screen.getByRole('heading', { name: 'Penayangan per 3 hari' })).toBeInTheDocument()
    expect(container.querySelectorAll('.chart-bar')).toHaveLength(30)
    expect(container.querySelector('.chart-bar')).toHaveAttribute('title', '14 Agu – 16 Agu: 3 tayangan')
  })
})
