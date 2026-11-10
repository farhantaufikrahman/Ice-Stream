import { useState } from 'react'
import { applyTheme, currentTheme, type Theme } from '../lib/theme'

/** Tombol bulat setengah terisi dari desain, untuk ganti mode gelap/terang. */
export function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(currentTheme)
  const label = theme === 'dark' ? 'Ganti ke mode terang' : 'Ganti ke mode gelap'
  const toggle = () => {
    const next: Theme = theme === 'dark' ? 'light' : 'dark'
    applyTheme(next)
    setTheme(next)
  }
  return (
    <button type="button" className="icon-btn" onClick={toggle} aria-label={label} title={label}>
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
        <circle cx="12" cy="12" r="9" />
        <path d="M12 3a9 9 0 0 1 0 18z" fill="currentColor" />
      </svg>
    </button>
  )
}
