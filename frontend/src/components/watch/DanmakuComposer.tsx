import { useEffect, useState, type FormEvent, type RefObject } from 'react'
import { Link, useLocation } from 'react-router'
import { errorMessage } from '../../api/client'
import { sendDanmaku } from '../../api/endpoints'
import type { Danmaku } from '../../api/types'
import { DANMAKU_COLORS, formatClock, MAX_DANMAKU_CHARS } from '../../lib/danmaku'
import { charCount } from '../../lib/upload'

interface Props {
  videoId: string
  videoRef: RefObject<HTMLVideoElement | null>
  loggedIn: boolean
  /** Danmaku tampil di layar atau tidak (tombol "Danmaku aktif"). */
  visible: boolean
  onToggle: () => void
  onSent: (d: Danmaku) => void
}

/** Baris kirim danmaku di bawah player (desain Watch.dc.html). */
export function DanmakuComposer({ videoId, videoRef, loggedIn, visible, onToggle, onSent }: Props) {
  const location = useLocation()
  const [text, setText] = useState('')
  const [color, setColor] = useState<string>(DANMAKU_COLORS[0].value)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const second = useVideoSecond(videoRef)
  const count = charCount(text.trim())
  const tooLong = count > MAX_DANMAKU_CHARS

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    const body = text.trim()
    const video = videoRef.current
    if (!body || tooLong || !video) return
    setBusy(true)
    setError(null)
    try {
      const d = await sendDanmaku(videoId, {
        body,
        position_ms: Math.floor(video.currentTime * 1000),
        color,
        mode: 'SCROLL',
      })
      setText('')
      onSent(d)
    } catch (err) {
      // Misalnya 429: server membatasi 3 danmaku per 10 detik per user.
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  const toggle = (
    <button type="button" className={visible ? 'btn btn-small dm-toggle is-on' : 'btn btn-small dm-toggle'} onClick={onToggle} aria-pressed={visible}>
      <ChatIcon />
      {visible ? 'Danmaku aktif' : 'Danmaku mati'}
    </button>
  )

  if (!loggedIn) {
    return (
      <div className="dm-bar">
        {toggle}
        <span className="muted small">
          <Link to={`/login?next=${encodeURIComponent(location.pathname)}`}>Masuk</Link> untuk mengirim danmaku.
        </span>
      </div>
    )
  }

  return (
    <>
      <form className="dm-bar" onSubmit={onSubmit}>
        {toggle}
        <div className="dm-colors" role="radiogroup" aria-label="Warna danmaku">
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
        <label htmlFor="dm-input" className="sr-only">
          Tulis danmaku
        </label>
        <input
          id="dm-input"
          type="text"
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder={`Kirim danmaku… (muncul di detik ${formatClock(second * 1000)})`}
          aria-invalid={tooLong || undefined}
          autoComplete="off"
        />
        {count > MAX_DANMAKU_CHARS - 20 && (
          <span className={tooLong ? 'counter is-over' : 'counter'}>
            {count}/{MAX_DANMAKU_CHARS}
          </span>
        )}
        <button type="submit" className="btn btn-primary" disabled={busy || !text.trim() || tooLong}>
          {busy ? 'Mengirim…' : 'Kirim'}
        </button>
      </form>
      {error && (
        <p className="field-error dm-error" role="alert">
          {error}
        </p>
      )}
    </>
  )
}

/** Detik video sekarang, diperbarui oleh event timeupdate (sekitar 4 kali per detik). */
function useVideoSecond(videoRef: RefObject<HTMLVideoElement | null>): number {
  const [second, setSecond] = useState(0)
  useEffect(() => {
    const video = videoRef.current
    if (!video) return
    const update = () => setSecond(Math.floor(video.currentTime))
    video.addEventListener('timeupdate', update)
    video.addEventListener('seeked', update)
    return () => {
      video.removeEventListener('timeupdate', update)
      video.removeEventListener('seeked', update)
    }
  }, [videoRef])
  return second
}

function ChatIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />
    </svg>
  )
}
