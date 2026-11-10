// Menambah matcher seperti toBeInTheDocument() ke expect milik Vitest.
// https://github.com/testing-library/jest-dom#with-vitest
import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'

afterEach(() => cleanup())
