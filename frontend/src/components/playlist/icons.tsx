/** Gembok kecil untuk playlist dan Tonton nanti yang privat. */
export function LockIcon() {
  return (
    <svg className="inline-icon" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <rect x="4" y="11" width="16" height="10" rx="2" />
      <path d="M8 11V7a4 4 0 0 1 8 0v4" />
    </svg>
  )
}

/** Ikon daftar putar: tiga garis dan segitiga play. */
export function PlaylistIcon() {
  return (
    <svg className="inline-icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" aria-hidden="true">
      <path d="M4 6h16M4 12h10M4 18h10" />
      <path d="M17 14v6l4-3z" fill="currentColor" />
    </svg>
  )
}
