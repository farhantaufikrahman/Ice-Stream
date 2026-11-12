import { describe, expect, it } from 'vitest'
import { addTags, categoryLabel, detailsChanges, isCategory, MAX_TAGS, normalizeTag, sameTags, tagError } from './categories'

describe('categoryLabel / isCategory', () => {
  it('mengenal kategori dari backend', () => {
    expect(categoryLabel('game')).toBe('Game')
    expect(categoryLabel('olahraga')).toBe('Olahraga')
    expect(isCategory('musik')).toBe(true)
  })
  it('menolak kategori kosong atau tidak dikenal', () => {
    expect(categoryLabel('')).toBeNull()
    expect(categoryLabel(undefined)).toBeNull()
    expect(categoryLabel('film')).toBeNull()
    expect(isCategory('Game')).toBe(false)
    expect(isCategory(null)).toBe(false)
  })
})

describe('tag', () => {
  it('membuang # dan spasi', () => {
    expect(normalizeTag('  #esKrim ')).toBe('esKrim')
    expect(normalizeTag('café')).toBe('café')
  })

  it('tagError mengikuti aturan server', () => {
    expect(tagError('esKrim')).toBeNull()
    expect(tagError('2026')).toBeNull()
    expect(tagError('アニメ')).toBeNull()
    expect(tagError('a')).not.toBeNull()
    expect(tagError('es-krim')).not.toBeNull()
    expect(tagError('es_krim')).not.toBeNull()
    expect(tagError('es🍦')).not.toBeNull()
    expect(tagError('a'.repeat(31))).not.toBeNull()
  })

  it('addTags memecah teks tempelan dan melewati tag dobel', () => {
    expect(addTags(['esKrim'], '#ESKRIM, anakKos #kuliner')).toEqual({ ok: true, tags: ['esKrim', 'anakKos', 'kuliner'] })
    expect(addTags([], '   ')).toEqual({ ok: true, tags: [] })
  })

  it('addTags menolak tag tidak valid dan lebih dari 10 tag', () => {
    expect(addTags([], 'oke x')).toMatchObject({ ok: false })
    const ten = Array.from({ length: MAX_TAGS }, (_, i) => `tag${i}`)
    expect(addTags(ten.slice(0, 9), 'tag9')).toMatchObject({ ok: true })
    expect(addTags(ten, 'lagi')).toEqual({ ok: false, message: 'Maksimal 10 tag.' })
  })

  it('sameTags membandingkan isi dan urutan', () => {
    expect(sameTags(['a', 'b'], ['a', 'b'])).toBe(true)
    expect(sameTags(['a', 'b'], ['b', 'a'])).toBe(false)
    expect(sameTags([], [])).toBe(true)
  })
})

describe('detailsChanges', () => {
  const saved = { title: 'Gelato', description: '', category: 'kuliner', tags: ['esKrim'] }
  it('kosong jika tidak ada yang berubah', () => {
    expect(detailsChanges(saved, { ...saved, title: ' Gelato ' })).toEqual({})
  })
  it('hanya field yang berubah', () => {
    expect(detailsChanges(saved, { ...saved, category: '', tags: [] })).toEqual({ category: '', tags: [] })
    expect(detailsChanges(saved, { ...saved, description: ' baru ', tags: ['esKrim', 'kos'] })).toEqual({
      description: 'baru',
      tags: ['esKrim', 'kos'],
    })
  })
  it('visibilitas, bahasa, dan draf (langkah 26) hanya jika diisi dan berubah', () => {
    const v = { ...saved, visibility: 'PUBLIC' as const, language: '' as const, draft: true }
    expect(detailsChanges(v, { ...saved })).toEqual({})
    expect(detailsChanges(v, { ...v, visibility: 'PRIVATE', language: 'ja', draft: false })).toEqual({
      visibility: 'PRIVATE',
      language: 'ja',
      draft: false,
    })
  })
})
