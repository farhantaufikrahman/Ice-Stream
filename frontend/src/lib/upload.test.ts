import { describe, expect, it } from 'vitest'
import {
  canDelete,
  charCount,
  checkVideoFile,
  formatBytes,
  formatEta,
  isProcessing,
  MAX_UPLOAD_BYTES,
  titleError,
  titleFromFileName,
} from './upload'

describe('checkVideoFile', () => {
  it('menerima tipe yang didukung', () => {
    expect(checkVideoFile({ name: 'a.mp4', size: 10, type: 'video/mp4' })).toEqual({ ok: true, contentType: 'video/mp4' })
  })

  it('memakai ekstensi jika browser tidak mengisi type', () => {
    expect(checkVideoFile({ name: 'film.MKV', size: 10, type: '' })).toEqual({ ok: true, contentType: 'video/x-matroska' })
  })

  it('menolak format lain, file kosong, dan file terlalu besar', () => {
    expect(checkVideoFile({ name: 'a.avi', size: 10, type: 'video/x-msvideo' }).ok).toBe(false)
    expect(checkVideoFile({ name: 'a.mp4', size: 0, type: 'video/mp4' }).ok).toBe(false)
    expect(checkVideoFile({ name: 'a.mp4', size: MAX_UPLOAD_BYTES + 1, type: 'video/mp4' }).ok).toBe(false)
  })
})

describe('judul', () => {
  it('membuat judul awal dari nama file', () => {
    expect(titleFromFileName('gelato-3_rasa--final.mp4')).toBe('gelato 3 rasa final')
    expect(titleFromFileName('.mp4')).toBe('Video tanpa judul')
    expect(charCount(titleFromFileName(`${'a'.repeat(150)}.mp4`))).toBe(100)
  })

  it('menghitung karakter seperti server (emoji = 1)', () => {
    expect(charCount('🍦')).toBe(1)
    expect(titleError('🍦'.repeat(100))).toBeNull()
    expect(titleError('   ')).toBe('Judul wajib diisi.')
    expect(titleError('a'.repeat(101))).not.toBeNull()
  })
})

describe('format', () => {
  it('formatBytes', () => {
    expect(formatBytes(512)).toBe('512 B')
    expect(formatBytes(640 * 1024 * 1024)).toBe('640 MB')
    expect(formatBytes(1.5 * 1024 * 1024 * 1024)).toBe('1,5 GB')
  })

  it('formatEta', () => {
    expect(formatEta(30)).toBe('sisa kurang dari 1 menit')
    expect(formatEta(130)).toBe('sisa sekitar 2 menit')
    expect(formatEta(Number.POSITIVE_INFINITY)).toBe('')
  })
})

describe('status', () => {
  it('isProcessing dan canDelete', () => {
    expect(isProcessing('PROCESSING')).toBe(true)
    expect(isProcessing('READY')).toBe(false)
    expect(canDelete('PROCESSING')).toBe(false)
    expect(canDelete('UPLOADED')).toBe(false)
    expect(canDelete('FAILED')).toBe(true)
  })
})
