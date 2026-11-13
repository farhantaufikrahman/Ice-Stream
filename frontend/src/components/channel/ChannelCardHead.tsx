import { Link } from 'react-router'
import type { Owner } from '../../api/types'
import { useChannel } from '../../hooks/useChannel'
import { formatCount } from '../../lib/format'
import { Avatar } from '../Avatar'
import { SubscribeButton } from './SubscribeButton'

/**
 * Avatar, nama, jumlah pengikut, dan tombol Ikuti di kartu kanal
 * (desain Watch.dc.html dan Live.dc.html). Jumlah pengikut tidak ada di
 * data video maupun siaran, jadi profil kanal diambil terpisah lewat
 * GET /users/:username.
 */
export function ChannelCardHead({ owner }: { owner: Owner }) {
  const { channel, isOwn, applySubscription } = useChannel(owner.username)
  const name = owner.display_name || owner.username
  const href = `/channel/${encodeURIComponent(owner.username)}`
  return (
    <>
      <Link to={href} className="channel-avatar-link" aria-label={`Kanal ${name}`}>
        <Avatar name={name} url={owner.avatar_url} size={52} />
      </Link>
      <div className="channel-text">
        <Link to={href} className="channel-name">
          {name}
        </Link>
        <span className="muted small">
          @{owner.username}
          {channel && ` · ${formatCount(channel.user.subscriber_count)} pengikut`}
        </span>
      </div>
      {channel && (
        <SubscribeButton
          username={owner.username}
          subscribed={channel.viewer_subscribed ?? false}
          subscriberCount={channel.user.subscriber_count}
          isOwn={isOwn}
          onChange={applySubscription}
        />
      )}
    </>
  )
}
