import type { PagedVideos } from '../hooks/usePagedVideos'
import { ErrorBox } from './ErrorBox'
import { VideoCard } from './VideoCard'

interface Props {
  feed: PagedVideos
  empty: string
  /** Sembunyikan avatar dan nama pemilik (halaman kanal). */
  hideOwner?: boolean
}

/** Grid video + tombol "Muat lagi" untuk halaman berikutnya. */
export function VideoGrid({ feed, empty, hideOwner = false }: Props) {
  const { videos, loading, error, hasMore, loadMore, retry } = feed
  return (
    <>
      {videos.length > 0 && (
        <div className="video-grid">
          {videos.map((v) => (
            <VideoCard key={v.id} video={v} hideOwner={hideOwner} />
          ))}
        </div>
      )}
      {!loading && !error && videos.length === 0 && empty && <p className="muted center">{empty}</p>}
      {error && <ErrorBox message={error} onRetry={retry} />}
      {loading && <p className="muted center">Memuat…</p>}
      {!loading && !error && hasMore && (
        <div className="center">
          <button type="button" className="btn" onClick={loadMore}>
            Muat lagi
          </button>
        </div>
      )}
    </>
  )
}
