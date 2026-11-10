/// <reference types="vite/client" />

// Variabel VITE_* yang dipakai aplikasi (lihat .env.example).
// https://vite.dev/guide/env-and-mode#intellisense-for-typescript
interface ImportMetaEnv {
  readonly VITE_API_BASE_URL?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
