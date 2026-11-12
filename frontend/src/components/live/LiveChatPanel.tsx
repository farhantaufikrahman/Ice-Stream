import { useLayoutEffect, useRef, useState, type FormEvent } from 'react'
import { Link, useLocation } from 'react-router'
import { errorMessage } from '../../api/client'
import { sendLiveChat } from '../../api/endpoints'
import type { LiveChatMessage, LiveChatOptions } from '../../api/types'
import { DANMAKU_COLORS, MAX_DANMAKU_CHARS } from '../../lib/danmaku'
import { charCount } from '../../lib/upload'

interface Props {
  streamId: string
  ownerId: string
  /** id user yang sedang login, kosong jika belum login. */
  viewerId: string
  /** false jika siaran sudah selesai: chat hanya bisa dibaca. */
  live: boolean
  messages: LiveChatMessage[]
  loaded: boolean
  onSent: (m: LiveChatMessage) => void
  /**
   * Pilihan chat kanal saat ini (langkah 27): mode lambat dan khusus
   * pengikut. Kreator bisa mengubahnya di tengah siaran; halaman membaca
   * ulang siaran setiap 10 detik.
   */
  options?: LiveChatOptions
}

/** Warna nama pengirim, bergiliran seperti desain Live.dc.html. */
const NAME_COLORS = ['var(--accent-text)', 'var(--amber-text)', 'var(--pink-text)', 'var(--soft)']

function nameColor(userId: string): string {
  let n = 0
  for (const ch of userId) n = (n + ch.charCodeAt(0)) % 997
  return NAME_COLORS[n % NAME_COLORS.length]
}

/**
 * Panel "Chat langsung" di samping player (desain Live.dc.html). Setiap
 * pesan juga melayang sebagai danmaku di atas video (diatur halaman).
 *
 * Daftar otomatis turun ke pesan terbaru, kecuali penonton sedang
 * menggulir ke atas membaca pesan lama.
 */
export function LiveChatPanel({ streamId, ownerId, viewerId, live, messages, loaded, onSent, options }: Props) {
  const location = useLocation()
  const listRef = useRef<HTMLDivElement>(null)
  const stickRef = useRef(true)
  const [text, setText] = useState('')
  const [color, setColor] = useState<string>(DANMAKU_COLORS[0].value)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const count = charCount(text.trim())
  const tooLong = count > MAX_DANMAKU_CHARS

  useLayoutEffect(() => {
    const el = listRef.current
    if (el && stickRef.current) el.scrollTop = el.scrollHeight
  }, [messages])

  const onScroll = () => {
    const el = listRef.current
    if (el) stickRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40
  }

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    const body = text.trim()
    if (!body || tooLong) return
    setBusy(true)
    setError(null)
    try {
      const m = await sendLiveChat(streamId, body, color)
      setText('')
      stickRef.current = true
      onSent(m)
    } catch (err) {
      // Misalnya 429 (mode lambat), 403 (khusus pengikut), atau 409
      // (siaran baru saja selesai).
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <aside className="live-chat" aria-labelledby="live-chat-title">
      <div className="live-chat-head">
        <h2 id="live-chat-title" className="live-chat-title">
          Chat langsung
        </h2>
        <span className="muted small">{live ? chatRules(options) : 'Siaran selesai'}</span>
      </div>
      <div ref={listRef} className="live-chat-list" onScroll={onScroll} aria-live="polite" aria-relevant="additions">
        {!loaded && <p className="muted small">Memuat chat…</p>}
        {loaded && messages.length === 0 && <p className="muted small">{live ? 'Belum ada pesan. Sapa kreatornya!' : 'Tidak ada pesan di siaran ini.'}</p>}
        {messages.map((m) => {
          const userId = m.user?.id ?? ''
          const name = m.user?.display_name || m.user?.username || 'Penonton'
          const badge = userId && userId === viewerId ? 'Kamu' : userId === ownerId ? 'Kreator' : ''
          return (
            <p key={m.id} className="live-chat-msg">
              {m.user ? (
                <Link to={`/channel/${encodeURIComponent(m.user.username)}`} className="live-chat-name" style={{ color: badge === 'Kamu' ? 'var(--fg)' : nameColor(userId) }}>
                  {name}
                </Link>
              ) : (
                <span className="live-chat-name">{name}</span>
              )}
              {badge && <span className="live-chat-badge">{badge}</span>} <span>{m.body}</span>
            </p>
          )
        })}
      </div>
      {!live ? null : viewerId ? (
        <form className="live-chat-form" onSubmit={onSubmit}>
          <div className="dm-colors" role="radiogroup" aria-label="Warna pesan di atas video">
            {DANMAKU_COLORS.map((c) => (
              <button
                key={c.value}
                type="button"
                role="radio"
                aria-checked={color === c.value}
                aria-label={c.name}
                title={c.name}
                className={color === c.value ? 'dm-color is-picked' : 'dm-color'}
                style={{ background: c.value }}
                onClick={() => setColor(c.value)}
              />
            ))}
          </div>
          <div className="live-chat-input">
            <label htmlFor="live-chat-text" className="sr-only">
              Tulis pesan chat
            </label>
            <input
              id="live-chat-text"
              type="text"
              value={text}
              onChange={(e) => setText(e.target.value)}
              placeholder="Kirim pesan…"
              aria-invalid={tooLong || undefined}
              autoComplete="off"
            />
            <button type="submit" className="btn btn-primary" disabled={busy || !text.trim() || tooLong}>
              Kirim
            </button>
          </div>
          {options?.followers_only && viewerId !== ownerId && (
            <span className="muted small">Hanya pengikut kanal ini yang bisa mengirim pesan.</span>
          )}
          {count > MAX_DANMAKU_CHARS - 20 && (
            <span className={tooLong ? 'counter is-over' : 'counter'}>
              {count}/{MAX_DANMAKU_CHARS}
            </span>
          )}
          {error && (
            <p className="field-error" role="alert">
              {error}
            </p>
          )}
        </form>
      ) : (
        <p className="live-chat-form muted small">
          <Link to={`/login?next=${encodeURIComponent(location.pathname)}`}>Masuk</Link> untuk ikut chat.
        </p>
      )}
    </aside>
  )
}

/** Keterangan di kepala panel, misalnya "Mode lambat · 3 detik · Khusus pengikut". */
function chatRules(options: LiveChatOptions | undefined): string {
  if (!options) return ''
  const parts: string[] = []
  if (options.slow_mode) parts.push('Mode lambat · 3 detik')
  if (options.followers_only) parts.push('Khusus pengikut')
  return parts.join(' · ')
}
