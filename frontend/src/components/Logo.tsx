import { useId } from 'react'

/**
 * Logo Ice-Stream dari desain "Ice-Stream UI": cone es krim berkepala TV
 * (sama dengan /mnt/project-files/design/ice-stream-logo.svg). Ditulis
 * sebagai komponen supaya bisa diberi ukuran, dan id clipPath dibuat unik
 * (useId) karena logo bisa muncul dua kali di satu halaman.
 */
export function LogoMark({ size = 36, title }: { size?: number; title?: string }) {
  const clip = `cone-${useId()}`
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 64 64"
      role={title ? 'img' : undefined}
      aria-label={title}
      aria-hidden={title ? undefined : true}
    >
      <defs>
        <clipPath id={clip}>
          <path d="M15 41 L49 41 L32 62 Z" />
        </clipPath>
      </defs>
      <g stroke="#4FE3C1" strokeWidth="3" strokeLinecap="round">
        <line x1="25" y1="14" x2="19" y2="5" />
        <line x1="39" y1="14" x2="45" y2="5" />
      </g>
      <circle cx="19" cy="5" r="3" fill="#F5B451" />
      <circle cx="45" cy="5" r="3" fill="#F5B451" />
      <path d="M15 41 L49 41 L32 62 Z" fill="#F5B451" />
      <g clipPath={`url(#${clip})`} stroke="#C9862F" strokeWidth="2">
        <line x1="10" y1="44" x2="34" y2="68" />
        <line x1="18" y1="38" x2="44" y2="64" />
        <line x1="28" y1="36" x2="52" y2="60" />
        <line x1="54" y1="44" x2="30" y2="68" />
        <line x1="46" y1="38" x2="20" y2="64" />
        <line x1="36" y1="36" x2="12" y2="60" />
      </g>
      <rect x="9" y="13" width="46" height="31" rx="10" fill="#4FE3C1" />
      <rect x="18" y="38" width="6" height="11" rx="3" fill="#4FE3C1" />
      <rect x="39" y="38" width="6" height="8" rx="3" fill="#4FE3C1" />
      <rect x="15" y="18.5" width="34" height="20" rx="5" fill="#0B1220" />
      <path d="M29 23.5 L37.5 28.5 L29 33.5 Z" fill="#4FE3C1" />
    </svg>
  )
}

/** Tulisan "Ice-Stream" dengan "-Stream" berwarna mint. */
export function Wordmark({ size = 22 }: { size?: number }) {
  return (
    <span className="wordmark" style={{ fontSize: size }}>
      Ice<span className="wordmark-accent">-Stream</span>
    </span>
  )
}
