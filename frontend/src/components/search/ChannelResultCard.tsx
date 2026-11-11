import { useState } from 'react'
import { Link } from 'react-router'
import type { ChannelResult } from '../../api/types'
import { useAuth } from '../../auth/useAuth'
import { formatCount } from '../../lib/format'
import { Avatar } from '../Avatar'
import { SubscribeButton } from '../channel/SubscribeButton'

/**
 * Kartu kreator di hasil pencarian (desain Search.dc.html): avatar besar,
 * label KREATOR, nama, jumlah pengikut dan bio, lalu tombol Ikuti.
 *
 * Bagian "96 video" di desain belum ditampilkan karena API pencarian
 * kreator belum membawa jumlah video.
 */
export function ChannelResultCard({ channel }: { channel: ChannelResult }) {
  const { user } = useAuth()
  // Status Ikuti disimpan di sini supaya tombol dan jumlah pengikut
  // langsung berubah tanpa memuat ulang hasil pencarian.
  const [sub, setSub] = useState({ subscribed: channel.viewer_subscribed ?? false, count: channel.subscriber_count })
  const name = channel.display_name || channel.username
  const href = `/channel/${encodeURIComponent(channel.username)}`
  const bio = channel.bio.split('\n')[0]

  return (
    <article className="channel-result">
      <Link to={href} className="channel-result-avatar" tabIndex={-1} aria-hidden="true">
        <Avatar name={name} url={channel.avatar_url} size={72} />
      </Link>
      <div className="channel-result-text">
        <span className="kicker">Kreator</span>
        <Link to={href} className="channel-result-name">
          {name}
        </Link>
        <span className="muted small">
          @{channel.username} · {formatCount(sub.count)} pengikut
          {bio && ` · ${bio}`}
        </span>
      </div>
      <SubscribeButton
        username={channel.username}
        subscribed={sub.subscribed}
        subscriberCount={sub.count}
        isOwn={user?.id === channel.id}
        onChange={(r) => setSub({ subscribed: r.subscribed, count: r.subscriber_count })}
      />
    </article>
  )
}
