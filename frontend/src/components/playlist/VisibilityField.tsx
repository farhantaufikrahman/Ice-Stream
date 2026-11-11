import type { PlaylistVisibility } from '../../api/types'

interface Props {
  /** Atribut name untuk radio, harus unik di satu halaman. */
  name: string
  value: PlaylistVisibility
  onChange: (value: PlaylistVisibility) => void
  disabled?: boolean
}

const OPTIONS: { value: PlaylistVisibility; label: string; hint: string }[] = [
  { value: 'PRIVATE', label: 'Privat', hint: 'Hanya kamu yang bisa melihat' },
  { value: 'PUBLIC', label: 'Publik', hint: 'Tampil di kanalmu, bisa dibuka siapa saja' },
]

/** Pilihan visibilitas playlist (dua radio). */
export function VisibilityField({ name, value, onChange, disabled }: Props) {
  return (
    <fieldset className="field visibility-field" disabled={disabled}>
      <legend>Visibilitas</legend>
      {OPTIONS.map((o) => (
        <label key={o.value} className={value === o.value ? 'radio-card is-active' : 'radio-card'}>
          <input type="radio" name={name} value={o.value} checked={value === o.value} onChange={() => onChange(o.value)} />
          <span className="radio-card-text">
            <span className="radio-card-label">{o.label}</span>
            <span className="muted small">{o.hint}</span>
          </span>
        </label>
      ))}
    </fieldset>
  )
}
