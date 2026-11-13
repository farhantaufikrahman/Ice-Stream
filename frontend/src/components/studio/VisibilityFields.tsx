import type { VideoVisibility } from '../../api/types'
import { VISIBILITIES } from '../../lib/visibility'

interface Props {
  idPrefix: string
  value: VideoVisibility
  onChange: (value: VideoVisibility) => void
  disabled?: boolean
}

/** Pilihan "Visibilitas" (Publik / Tidak publik / Pribadi) dari desain Upload.dc.html. */
export function VisibilityFields({ idPrefix, value, onChange, disabled }: Props) {
  return (
    <fieldset className="choice-group" disabled={disabled}>
      <legend>Visibilitas</legend>
      {VISIBILITIES.map((v) => (
        <label key={v.value} htmlFor={`${idPrefix}-vis-${v.value}`} className={value === v.value ? 'choice is-selected' : 'choice'}>
          <input
            id={`${idPrefix}-vis-${v.value}`}
            type="radio"
            name={`${idPrefix}-vis`}
            value={v.value}
            checked={value === v.value}
            onChange={() => onChange(v.value)}
          />
          <span className="choice-text">
            <strong>{v.label}</strong>
            <span className="muted small">{v.description}</span>
          </span>
        </label>
      ))}
    </fieldset>
  )
}
