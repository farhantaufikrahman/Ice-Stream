import { DURATIONS, SEARCH_SORTS, type DurationFilter, type SearchSort } from '../../lib/discover'

interface Props {
  sort: SearchSort
  duration: DurationFilter
  onSort: (sort: SearchSort) => void
  onDuration: (duration: DurationFilter) => void
}

/**
 * Baris "Urutkan" dan "Durasi" di halaman pencarian (desain Search.dc.html).
 *
 * Tombol memakai aria-pressed (tombol on/off), bukan radio: pembaca layar
 * menyebut "Terbaru, tombol, ditekan". Durasi bisa dilepas dengan menekan
 * tombol yang aktif sekali lagi, jadi tidak perlu tombol "Semua durasi".
 */
export function SearchFilters({ sort, duration, onSort, onDuration }: Props) {
  return (
    <div className="search-filters">
      <div className="filter-group" role="group" aria-labelledby="filter-sort-label">
        <span id="filter-sort-label" className="muted">
          Urutkan
        </span>
        {SEARCH_SORTS.map((s) => (
          <button
            key={s.value}
            type="button"
            aria-pressed={sort === s.value}
            className={sort === s.value ? 'filter-btn is-active' : 'filter-btn'}
            onClick={() => onSort(s.value)}
          >
            {s.label}
          </button>
        ))}
      </div>
      <span className="filter-divider" aria-hidden="true" />
      <div className="filter-group" role="group" aria-labelledby="filter-duration-label">
        <span id="filter-duration-label" className="muted">
          Durasi
        </span>
        {DURATIONS.map((d) => {
          const active = duration === d.value
          return (
            <button
              key={d.value}
              type="button"
              aria-pressed={active}
              className={active ? 'filter-btn is-picked' : 'filter-btn'}
              onClick={() => onDuration(active ? '' : d.value)}
            >
              {d.label}
            </button>
          )
        })}
      </div>
    </div>
  )
}
