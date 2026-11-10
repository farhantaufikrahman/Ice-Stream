import { useSearchParams } from 'react-router'
import { searchVideos } from '../api/endpoints'
import { VideoGrid } from '../components/VideoGrid'
import { usePagedVideos } from '../hooks/usePagedVideos'

export function SearchPage() {
  const [params] = useSearchParams()
  const q = (params.get('q') ?? '').trim()
  const feed = usePagedVideos(`search:${q}`, (token, signal) =>
    q ? searchVideos(q, token, signal) : Promise.resolve({ videos: [], next_page_token: '' }),
  )
  return (
    <>
      <h1 className="page-title">{q ? `Hasil untuk "${q}"` : 'Cari video'}</h1>
      <VideoGrid feed={feed} empty={q ? 'Tidak ada video yang cocok.' : 'Ketik kata kunci di kotak pencarian.'} />
    </>
  )
}
