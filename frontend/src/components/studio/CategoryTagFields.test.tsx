import { fireEvent, render, screen } from '@testing-library/react'
import { useRef, useState } from 'react'
import { describe, expect, it } from 'vitest'
import { CategoryTagFields, type TagFieldHandle } from './CategoryTagFields'

/** Pembungkus dengan state, seperti di halaman Unggah. */
function Harness({ initialTags = [] as string[] }) {
  const [category, setCategory] = useState('')
  const [tags, setTags] = useState(initialTags)
  const [flushed, setFlushed] = useState<string[] | null | undefined>(undefined)
  const ref = useRef<TagFieldHandle>(null)
  return (
    <>
      <button type="button" onClick={() => setFlushed(ref.current?.flush())}>
        Kirim
      </button>
      <output data-testid="flushed">{JSON.stringify(flushed === undefined ? 'belum' : flushed)}</output>
      <CategoryTagFields
        tagsRef={ref}
        idPrefix="t"
        category={category}
        tags={tags}
        onCategoryChange={setCategory}
        onTagsChange={setTags}
        emptyCategoryLabel="Pilih kategori"
      />
      <output data-testid="state">{JSON.stringify({ category, tags })}</output>
    </>
  )
}

const state = () => JSON.parse(screen.getByTestId('state').textContent ?? '{}') as { category: string; tags: string[] }

describe('CategoryTagFields', () => {
  it('memilih kategori dari daftar', () => {
    render(<Harness />)
    const select = screen.getByLabelText('Kategori')
    expect(screen.getByRole('option', { name: 'Pilih kategori' })).toBeInTheDocument()
    fireEvent.change(select, { target: { value: 'kuliner' } })
    expect(state().category).toBe('kuliner')
  })

  it('Enter, spasi, dan koma menambah tag; # dibuang; dobel dilewati', () => {
    render(<Harness />)
    const input = screen.getByLabelText('Tag')
    fireEvent.change(input, { target: { value: '#esKrim' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    fireEvent.change(input, { target: { value: 'anakKos' } })
    fireEvent.keyDown(input, { key: ' ' })
    fireEvent.change(input, { target: { value: 'ESKRIM' } })
    fireEvent.keyDown(input, { key: ',' })
    expect(state().tags).toEqual(['esKrim', 'anakKos'])
    expect(input).toHaveValue('')
    expect(screen.getByText('#esKrim')).toBeInTheDocument()
    expect(screen.getByText('2/10')).toBeInTheDocument()
  })

  it('tag salah tetap di kotak dengan pesan', () => {
    render(<Harness />)
    const input = screen.getByLabelText('Tag')
    fireEvent.change(input, { target: { value: 'es-krim' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(state().tags).toEqual([])
    expect(input).toHaveValue('es-krim')
    expect(input).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByText(/harus 2-30 huruf atau angka/)).toBeInTheDocument()
  })

  it('flush() saat form dikirim ikut menyimpan tag yang sedang diketik', () => {
    render(<Harness initialTags={['esKrim']} />)
    const input = screen.getByLabelText('Tag')
    fireEvent.change(input, { target: { value: 'kuliner' } })
    fireEvent.blur(input)
    expect(state().tags, 'pindah fokus saja tidak menambah tag').toEqual(['esKrim'])
    fireEvent.click(screen.getByRole('button', { name: 'Kirim' }))
    expect(screen.getByTestId('flushed')).toHaveTextContent('["esKrim","kuliner"]')
    expect(state().tags).toEqual(['esKrim', 'kuliner'])
  })

  it('flush() mengembalikan null jika teks yang diketik salah', () => {
    render(<Harness />)
    fireEvent.change(screen.getByLabelText('Tag'), { target: { value: 'x' } })
    fireEvent.click(screen.getByRole('button', { name: 'Kirim' }))
    expect(screen.getByTestId('flushed')).toHaveTextContent('null')
    expect(screen.getByText(/harus 2-30 huruf atau angka/)).toBeInTheDocument()
  })

  it('tombol × dan Backspace menghapus tag', () => {
    render(<Harness initialTags={['esKrim', 'anakKos', 'kuliner']} />)
    fireEvent.click(screen.getByRole('button', { name: 'Hapus tag anakKos' }))
    expect(state().tags).toEqual(['esKrim', 'kuliner'])
    fireEvent.keyDown(screen.getByLabelText('Tag'), { key: 'Backspace' })
    expect(state().tags).toEqual(['esKrim'])
  })

  it('menempel beberapa tag sekaligus', () => {
    render(<Harness />)
    const input = screen.getByLabelText('Tag')
    fireEvent.paste(input, { clipboardData: { getData: () => '#kuliner #esKrim, anakKos' } })
    expect(state().tags).toEqual(['kuliner', 'esKrim', 'anakKos'])
  })

  it('kotak tag terkunci setelah 10 tag', () => {
    render(<Harness initialTags={Array.from({ length: 10 }, (_, i) => `tag${i}`)} />)
    expect(screen.getByLabelText('Tag')).toBeDisabled()
    expect(screen.getByText('10/10')).toBeInTheDocument()
  })
})
