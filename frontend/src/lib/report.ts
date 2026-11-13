// Langkah 24b: pilihan alasan di dialog Laporkan. Nilai "value" sama
// dengan yang diterima gateway (backend/internal/httpserver/report.go).

export type ReportReason = 'spam' | 'sexual' | 'violence' | 'hate' | 'harassment' | 'misleading' | 'copyright' | 'other'

export const REPORT_REASONS: { value: ReportReason; label: string }[] = [
  { value: 'spam', label: 'Spam atau penipuan' },
  { value: 'sexual', label: 'Konten seksual' },
  { value: 'violence', label: 'Kekerasan atau konten berbahaya' },
  { value: 'hate', label: 'Ujaran kebencian' },
  { value: 'harassment', label: 'Perundungan atau pelecehan' },
  { value: 'misleading', label: 'Informasi yang menyesatkan' },
  { value: 'copyright', label: 'Pelanggaran hak cipta' },
  { value: 'other', label: 'Lainnya' },
]

/** Sama dengan kolom details VARCHAR2(500 CHAR) di server. */
export const MAX_REPORT_DETAILS = 500

/** Pesan kesalahan untuk keterangan, atau null jika boleh dikirim. */
export function reportDetailsError(reason: ReportReason | null, details: string, count: number): string | null {
  if (count > MAX_REPORT_DETAILS) return `Maksimal ${MAX_REPORT_DETAILS} karakter.`
  if (reason === 'other' && details.trim() === '') return 'Ceritakan singkat apa masalahnya.'
  return null
}

/** "spam" -> "Spam atau penipuan". Nilai yang tidak dikenal ditampilkan apa adanya. */
export function reportReasonLabel(reason: string): string {
  return REPORT_REASONS.find((r) => r.value === reason)?.label ?? reason
}
