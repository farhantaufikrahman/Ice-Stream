// Menambah matcher seperti toBeInTheDocument() ke expect milik Vitest.
// https://github.com/testing-library/jest-dom#with-vitest
import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'

afterEach(() => cleanup())

// jsdom (lingkungan browser palsu untuk tes) belum punya showModal() dan
// close() di <dialog>, yang dipakai komponen Modal. Versi sederhana ini
// cukup untuk tes: dialog dianggap terbuka selama atribut "open" ada.
if (typeof HTMLDialogElement !== 'undefined' && !HTMLDialogElement.prototype.showModal) {
  HTMLDialogElement.prototype.showModal = function showModal(this: HTMLDialogElement) {
    this.setAttribute('open', '')
  }
  HTMLDialogElement.prototype.close = function close(this: HTMLDialogElement) {
    this.removeAttribute('open')
  }
}
