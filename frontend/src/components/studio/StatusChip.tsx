import type { VideoStatus } from '../../api/types'

const chips: Record<VideoStatus, { label: string; tone: 'mint' | 'amber' | 'pink' }> = {
  PENDING_UPLOAD: { label: 'Belum diunggah', tone: 'pink' },
  UPLOADED: { label: 'Antre', tone: 'amber' },
  PROCESSING: { label: 'Diproses', tone: 'amber' },
  READY: { label: 'Siap', tone: 'mint' },
  FAILED: { label: 'Gagal', tone: 'pink' },
}

/** Label status video berwarna, seperti kolom Status di desain Studio. */
export function StatusChip({ status }: { status: VideoStatus }) {
  const chip = chips[status]
  return <span className={`chip chip-${chip.tone}`}>{chip.label}</span>
}
