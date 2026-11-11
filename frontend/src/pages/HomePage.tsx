import { useEffect, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { listFeed, listSubscriptions, listVideos } from '../api/endpoints'
import type { Profile } from '../api/types'
import { useAuth } from '../auth/useAuth'
import { Avatar } from '../components/Avatar'
import { VideoGrid } from '../components/VideoGrid'
import { usePagedVideos } from '../hooks/usePagedVideos'
import { CATEGORIES, categoryLabel, isCategory } from '../lib/categories'

/** Jumlah kanal di baris "Kanal yang kamu ikuti". */
const STRIP_COUNT = 24

/**
 * Beranda. Chip di atas grid mengikuti desain Home.dc.html: "Semua",
 * "Mengikuti" (setelah login, video terbaru dari kanal yang kamu ikuti),
 * lalu satu chip per kategori (langkah 23).
 *
 * Pilihan chip disimpan di URL (?category=game atau ?tab=mengikuti),
 * jadi tombol Back dan tautan yang dibagikan membuka chip yang sama.
 */
export function HomePage() {
  const { status } = useAuth()
  const [params, setParams] = useSearchParams()
  const following = status === 'authenticated' && params.get('tab') === 'mengikuti'
  const rawCategory = params.get('category')
  // Kategori yang tidak dikenal di URL diperlakukan seperti "Semua".
  const category = !following && isCategory(rawCategory) ? rawCategory : ''

  // Di HP chip bisa digeser; chip yang aktif (misalnya dari tautan
  // ?category=olahraga) digeser sampai terlihat.
  const chipsRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const active = chipsRef.current?.querySelector<HTMLElement>('[aria-selected="true"]')
    // jsdom (tes) tidak punya scrollIntoView, jadi dipanggil hanya jika ada.
    active?.scrollIntoView?.({ block: 'nearest', inline: 'nearest' })
  }, [category, following])

  const chip = (label: string, active: boolean, next: Record<string, string>) => (
    <button
      key={label}
      type="button"
      role="tab"
      aria-selected={active}
      className={active ? 'chip-tab is-active' : 'chip-tab'}
      onClick={() => setParams(next)}
    >
      {label}
    </button>
  )

  return (
    <>
      <div ref={chipsRef} className="chips chips-scroll" role="tablist" aria-label="Pilih daftar video">
        {chip('Semua', !following && !category, {})}
        {status === 'authenticated' && chip('Mengikuti', following, { tab: 'mengikuti' })}
        {CATEGORIES.map((c) => chip(c.label, category === c.value, { category: c.value }))}
      </div>
      {following ? <FollowingFeed /> : <LatestFeed category={category} />}
    </>
  )
}

function LatestFeed({ category }: { category: string }) {
  // key berbeda per kategori: daftar dimulai lagi dari halaman pertama.
  const feed = usePagedVideos(`home:${category}`, (token, signal) => listVideos(token, signal, category))
  const label = categoryLabel(category)
  return (
    <>
      <h1 className="page-title">{label ? `${label} terbaru` : 'Video terbaru'}</h1>
      <VideoGrid
        feed={feed}
        empty={label ? `Belum ada video di kategori ${label}.` : 'Belum ada video. Jadilah yang pertama mengunggah!'}
      />
    </>
  )
}

function FollowingFeed() {
  const feed = usePagedVideos('feed', listFeed)
  return (
    <>
      <SubscriptionStrip />
      <h1 className="page-title">Dari kanal yang kamu ikuti</h1>
      <VideoGrid feed={feed} empty="Belum ada video di sini. Buka halaman kanal yang kamu suka, lalu tekan Ikuti." />
    </>
  )
}

/** Baris avatar kanal yang diikuti (GET /me/subscriptions), menuju halaman kanal. */
function SubscriptionStrip() {
  const [channels, setChannels] = useState<Profile[] | null>(null)
  useEffect(() => {
    const ctrl = new AbortController()
    listSubscriptions('', ctrl.signal)
      .then((page) => setChannels(page.channels.slice(0, STRIP_COUNT)))
      // Baris ini hanya pelengkap: jika gagal, cukup tidak ditampilkan.
      .catch(() => setChannels([]))
    return () => ctrl.abort()
  }, [])
  if (!channels || channels.length === 0) return null
  return (
    <section className="subs-strip" aria-label="Kanal yang kamu ikuti">
      {channels.map((c) => {
        const name = c.display_name || c.username
        return (
          <Link key={c.id} to={`/channel/${encodeURIComponent(c.username)}`} className="subs-item" title={name}>
            <Avatar name={name} url={c.avatar_url} size={56} />
            <span className="subs-name">{name}</span>
          </Link>
        )
      })}
    </section>
  )
}
