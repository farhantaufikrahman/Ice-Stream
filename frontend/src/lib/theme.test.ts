import { afterEach, describe, expect, it } from 'vitest'
import { applyTheme, currentTheme } from './theme'

afterEach(() => {
  delete document.documentElement.dataset.theme
  localStorage.clear()
})

describe('theme', () => {
  it('default-nya gelap', () => {
    expect(currentTheme()).toBe('dark')
  })

  it('menyimpan pilihan di <html> dan localStorage', () => {
    applyTheme('light')
    expect(document.documentElement.dataset.theme).toBe('light')
    expect(localStorage.getItem('ice-theme')).toBe('light')
    expect(currentTheme()).toBe('light')
  })
})
