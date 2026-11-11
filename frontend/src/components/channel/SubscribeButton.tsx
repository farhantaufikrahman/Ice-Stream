import { useState } from 'react'
import { Link, useLocation } from 'react-router'
import { errorMessage } from '../../api/client'
import { setSubscription } from '../../api/endpoints'
import type { SubscriptionResult } from '../../api/types'
import { useAuth } from '../../auth/useAuth'

interface Props {
  username: string
  subscribed: boolean
  subscriberCount: number
  /** Kanal milik penonton sendiri: tampil "Kelola video" (ke Studio). */
  isOwn: boolean
  onChange: (r: SubscriptionResult) => void
}

/**
 * Tombol "Ikuti" / "Mengikuti" (desain Channel.dc.html dan Watch.dc.html):
 * stroberi saat belum mengikuti, abu-abu saat sudah.
 *
 * Optimistic update seperti tombol Suka: tampilan langsung berubah, lalu
 * dibetulkan dengan jawaban server, atau dikembalikan jika gagal.
 */
export function SubscribeButton({ username, subscribed, subscriberCount, isOwn, onChange }: Props) {
  const { status } = useAuth()
  const location = useLocation()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  if (isOwn) {
    return (
      <Link to="/studio" className="btn">
        Kelola video
      </Link>
    )
  }
  if (status !== 'authenticated') {
    return (
      <Link to={`/login?next=${encodeURIComponent(location.pathname + location.search)}`} className="btn btn-follow" title="Masuk untuk mengikuti kanal">
        Ikuti
      </Link>
    )
  }

  const toggle = async () => {
    const want = !subscribed
    const before: SubscriptionResult = { subscribed, subscriber_count: subscriberCount }
    onChange({ subscribed: want, subscriber_count: Math.max(0, subscriberCount + (want ? 1 : -1)) })
    setBusy(true)
    setError(null)
    try {
      onChange(await setSubscription(username, want))
    } catch (err) {
      onChange(before)
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <span className="follow">
      <button
        type="button"
        className={subscribed ? 'btn btn-follow is-following' : 'btn btn-follow'}
        onClick={toggle}
        disabled={busy}
        aria-pressed={subscribed}
      >
        {subscribed ? 'Mengikuti' : 'Ikuti'}
      </button>
      {error && (
        <span className="field-error" role="alert">
          {error}
        </span>
      )}
    </span>
  )
}
