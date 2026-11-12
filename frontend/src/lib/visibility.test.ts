import { describe, expect, it } from 'vitest'
import { isListed, languageLabel, thumbnailFileError, videoDate, visibilityLabel } from './visibility'

describe('visibility', () => {
  it('label visibilitas dan bahasa', () => {
    expect(visibilityLabel('UNLISTED')).toBe('Tidak publik')
    expect(languageLabel('ja')).toBe('日本語')
    expect(languageLabel('')).toBeNull()
    expect(languageLabel('fr')).toBeNull()
  })

  it('tanggal terbit lebih dulu, lalu tanggal unggah', () => {
    expect(videoDate({ created_at: 'a', published_at: 'b' })).toBe('b')
    expect(videoDate({ created_at: 'a' })).toBe('a')
  })

  it('hanya video READY, bukan draf, dan PUBLIC yang tampil di daftar', () => {
    expect(isListed({ status: 'READY', draft: false, visibility: 'PUBLIC' })).toBe(true)
    expect(isListed({ status: 'READY', draft: true, visibility: 'PUBLIC' })).toBe(false)
    expect(isListed({ status: 'READY', draft: false, visibility: 'UNLISTED' })).toBe(false)
    expect(isListed({ status: 'PROCESSING', draft: false, visibility: 'PUBLIC' })).toBe(false)
  })

  it('memeriksa jenis dan ukuran thumbnail', () => {
    expect(thumbnailFileError(new File(['x'], 'a.png', { type: 'image/png' }))).toBeNull()
    expect(thumbnailFileError(new File(['x'], 'a.gif', { type: 'image/gif' }))).toMatch(/JPEG/)
    const big = new File([new Uint8Array(2 * 1024 * 1024 + 1)], 'b.jpg', { type: 'image/jpeg' })
    expect(thumbnailFileError(big)).toMatch(/2 MB/)
  })
})
