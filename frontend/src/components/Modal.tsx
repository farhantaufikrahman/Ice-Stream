import { useEffect, useId, useRef, type ReactNode } from 'react'

interface Props {
  title: string
  /** Dipanggil saat dialog ditutup (tombol Esc atau onClose dari isi). */
  onClose: () => void
  /** true: Esc tidak menutup dialog (misalnya saat sedang menyimpan). */
  locked?: boolean
  children: ReactNode
}

/**
 * Dialog modal memakai elemen <dialog> bawaan browser. showModal() sudah
 * mengurus fokus, tombol Esc, dan latar gelap (::backdrop), jadi tidak perlu
 * library. Dokumentasi:
 * https://developer.mozilla.org/en-US/docs/Web/HTML/Reference/Elements/dialog
 *
 * Cara pakai: tampilkan komponen ini hanya saat dialog perlu terbuka,
 * misalnya {editing && <Modal ...>}. Dialog langsung terbuka saat muncul.
 */
export function Modal({ title, onClose, locked = false, children }: Props) {
  const ref = useRef<HTMLDialogElement>(null)
  const titleId = useId()

  useEffect(() => {
    const dialog = ref.current
    if (!dialog) return
    if (!dialog.open) dialog.showModal()
    return () => dialog.close()
  }, [])

  return (
    <dialog
      ref={ref}
      className="modal"
      aria-labelledby={titleId}
      onCancel={(e) => {
        // "cancel" = user menekan Esc. Dibatalkan, lalu komponen induk yang
        // memutuskan (dialog hilang saat induk berhenti menampilkannya).
        e.preventDefault()
        if (!locked) onClose()
      }}
    >
      <h2 id={titleId} className="modal-title">
        {title}
      </h2>
      {children}
    </dialog>
  )
}
