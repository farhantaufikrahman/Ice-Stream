import type { VideoOptions } from '../../hooks/useMyVideoOptions'

interface Props {
  options: VideoOptions
  value: string
  onChange: (videoId: string) => void
}

/** Pilihan "Semua video" atau satu video milik channel (langkah 26). */
export function VideoFilter({ options, value, onChange }: Props) {
  return (
    <div className="studio-filter">
      <label htmlFor="studio-video-filter" className="small muted">
        Video
      </label>
      <select id="studio-video-filter" value={value} onChange={(e) => onChange(e.target.value)}>
        <option value="">Semua video</option>
        {options.videos.map((v) => (
          <option key={v.id} value={v.id}>
            {v.title}
          </option>
        ))}
        {/* Video dari URL yang belum termuat tetap bisa dipilih. */}
        {value && !options.videos.some((v) => v.id === value) && <option value={value}>Video #{value}</option>}
      </select>
      {options.truncated && <span className="small muted">Hanya 240 video terbaru yang bisa dipilih.</span>}
      {options.error && <span className="small pink">Daftar video gagal dimuat: {options.error}</span>}
    </div>
  )
}
