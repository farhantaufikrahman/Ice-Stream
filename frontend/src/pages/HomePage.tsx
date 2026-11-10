import { VideoGrid } from '../components/VideoGrid'
import { listVideos } from '../api/endpoints'
import { usePagedVideos } from '../hooks/usePagedVideos'

export function HomePage() {
  const feed = usePagedVideos('home', listVideos)
  return (
    <>
      <h1 className="page-title">Video terbaru</h1>
      <VideoGrid feed={feed} empty="Belum ada video. Unggah video pertama lewat API dulu." />
    </>
  )
}
