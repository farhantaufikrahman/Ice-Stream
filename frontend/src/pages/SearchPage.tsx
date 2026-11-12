import type { ReactNode } from 'react'
import { useSearchParams } from 'react-router'
import { searchChannels, searchPlaylists, searchVideos } from '../api/endpoints'
import type { ChannelResult, Playlist } from '../api/types'
import { ErrorBox } from '../components/ErrorBox'
import { PlaylistCard } from '../components/playlist/PlaylistCard'
import { ChannelResultCard } from '../components/search/ChannelResultCard'
import { SearchFilters } from '../components/search/SearchFilters'
import { VideoGrid } from '../components/VideoGrid'
import { usePagedList, type PagedList } from '../hooks/usePagedList'
import { usePagedVideos } from '../hooks/usePagedVideos'
import { durationFromParam, searchSortFromParam, withParam, type DurationFilter, type SearchSort } from '../lib/discover'

type Tab = 'semua' | 'video' | 'kreator' | 'playlist'

const TABS: { id: Tab; label: string }[] = [
  { id: 'semua', label: 'Semua' },
  { id: 'video', label: 'Video' },
  { id: 'kreator', label: 'Kreator' },
  { id: 'playlist', label: 'Playlist' },
]

function tabFromParam(value: string | null): Tab {
  return TABS.find((t) => t.id === value)?.id ?? 'semua'
}

/**
 * Halaman pencarian /search?q=...&tab=... (desain Search.dc.html).
 *
 * Tab Semua: kreator paling cocok di atas, lalu video. Urutan dan filter
 * durasi (langkah 24a) berlaku untuk video, jadi hanya tampil di tab Semua
 * dan Video, dan disimpan di URL (?sort=views&duration=short). Tab Live
 * dan jumlah hasil per tab di desain belum ditampilkan karena backend
 * belum mendukungnya.
 */
export function SearchPage() {
  const [params, setParams] = useSearchParams()
  const q = (params.get('q') ?? '').trim()
  const tab = tabFromParam(params.get('tab'))
  const sort = searchSortFromParam(params.get('sort'))
  const duration = durationFromParam(params.get('duration'))

  if (!q) {
    return (
      <>
        <h1 className="page-title">Cari</h1>
        <p className="muted center">Ketik kata kunci di kotak pencarian.</p>
      </>
    )
  }

  const pickTab = (t: Tab) =>
    setParams(
      (p) => {
        const next = new URLSearchParams(p)
        if (t === 'semua') next.delete('tab')
        else next.set('tab', t)
        return next
      },
      { replace: true },
    )

  const setParam = (name: string, value: string, defaultValue = '') =>
    setParams((p) => withParam(p, name, value, defaultValue), { replace: true })
  const showFilters = tab === 'semua' || tab === 'video'
  const filters = { sort, duration }

  return (
    <div className="search-page">
      <h1 className="page-title">Hasil untuk "{q}"</h1>
      <div className="tabs" role="tablist" aria-label="Jenis hasil">
        {TABS.map((t) => (
          <button
            key={t.id}
            type="button"
            role="tab"
            id={`search-tab-${t.id}`}
            aria-selected={tab === t.id}
            aria-controls="search-panel"
            className={tab === t.id ? 'tab is-active' : 'tab'}
            onClick={() => pickTab(t.id)}
          >
            {t.label}
          </button>
        ))}
      </div>
      {showFilters && (
        <SearchFilters
          sort={sort}
          duration={duration}
          onSort={(v: SearchSort) => setParam('sort', v, 'relevance')}
          onDuration={(v: DurationFilter) => setParam('duration', v)}
        />
      )}
      {/* key: hasil dimuat ulang dari halaman pertama saat kata kunci atau tab berubah. */}
      <div role="tabpanel" id="search-panel" aria-labelledby={`search-tab-${tab}`} className="search-panel" key={`${tab}|${q}`}>
        {tab === 'semua' && <AllResults q={q} filters={filters} />}
        {tab === 'video' && <VideoResults q={q} filters={filters} />}
        {tab === 'kreator' && <ChannelResults q={q} />}
        {tab === 'playlist' && <PlaylistResults q={q} />}
      </div>
    </div>
  )
}

interface VideoFilters {
  sort: SearchSort
  duration: DurationFilter
}

function VideoResults({ q, filters: { sort, duration } }: { q: string; filters: VideoFilters }) {
  // sort dan duration ikut di key: ganti urutan = mulai lagi dari halaman 1.
  const feed = usePagedVideos(`search:${q}|${sort}|${duration}`, (token, signal) => searchVideos(q, token, signal, sort, duration))
  const empty = duration ? 'Tidak ada video yang cocok dengan durasi ini.' : 'Tidak ada video yang cocok.'
  return <VideoGrid feed={feed} empty={empty} />
}

/** Satu kreator paling cocok (seperti desain), lalu daftar video. */
function AllResults({ q, filters }: { q: string; filters: VideoFilters }) {
  const top = usePagedList<ChannelResult>((token, signal) =>
    searchChannels(q, token, signal, 1).then((p) => ({ items: p.channels, next: '' })),
  )
  return (
    <>
      {top.items[0] && <ChannelResultCard channel={top.items[0]} />}
      <VideoResults q={q} filters={filters} />
    </>
  )
}

function ChannelResults({ q }: { q: string }) {
  const list = usePagedList<ChannelResult>((token, signal) =>
    searchChannels(q, token, signal).then((p) => ({ items: p.channels, next: p.next_page_token })),
  )
  return (
    <ResultList list={list} empty="Tidak ada kreator yang cocok.">
      <div className="channel-results">
        {list.items.map((c) => (
          <ChannelResultCard key={c.id} channel={c} />
        ))}
      </div>
    </ResultList>
  )
}

function PlaylistResults({ q }: { q: string }) {
  const list = usePagedList<Playlist>((token, signal) =>
    searchPlaylists(q, token, signal).then((p) => ({ items: p.playlists, next: p.next_page_token })),
  )
  return (
    <ResultList list={list} empty="Tidak ada playlist publik yang cocok.">
      <div className="video-grid">
        {list.items.map((p) => (
          <PlaylistCard key={p.id} playlist={p} showOwner />
        ))}
      </div>
    </ResultList>
  )
}

/** Pesan kosong, error, Memuat…, dan tombol Muat lagi (sama seperti VideoGrid). */
function ResultList<T>({ list, empty, children }: { list: PagedList<T>; empty: string; children: ReactNode }) {
  return (
    <>
      {list.items.length > 0 && children}
      {!list.loading && !list.error && list.items.length === 0 && <p className="muted center">{empty}</p>}
      {list.error && <ErrorBox message={list.error} onRetry={list.retry} />}
      {list.loading && <p className="muted center">Memuat…</p>}
      {!list.loading && !list.error && list.hasMore && (
        <div className="center">
          <button type="button" className="btn" onClick={list.loadMore}>
            Muat lagi
          </button>
        </div>
      )}
    </>
  )
}
