/// <reference types="vitest/config" />
import react from '@vitejs/plugin-react'
import { defineConfig, loadEnv } from 'vite'

// Dokumentasi: https://vite.dev/config/ dan https://vite.dev/config/server-options#server-proxy
export default defineConfig(({ mode }) => {
  // Nilai tanpa awalan VITE_ hanya dibaca di sini (Node.js), tidak pernah
  // masuk ke kode yang dikirim ke browser.
  const env = loadEnv(mode, process.cwd(), '')
  const target = env.API_PROXY_TARGET || 'http://127.0.0.1:4000'
  const host = env.API_PROXY_HOST || 'api.ice-stream.localhost'

  return {
    plugins: [react()],
    server: {
      port: 5173,
      // Port 5173 sudah diizinkan CORS di RustFS (untuk HLS). Jika terpakai,
      // lebih baik gagal daripada diam-diam pindah ke 5174.
      strictPort: true,
      proxy: {
        // Browser memanggil http://localhost:5173/api/v1/..., lalu Vite
        // meneruskannya ke Gateway Kubernetes. Bagi browser, API dan halaman
        // berada di origin yang sama, jadi cookie refresh token (SameSite=Lax)
        // ikut terkirim dan tidak perlu CORS.
        //
        // Gateway memilih HTTPRoute berdasarkan header Host, jadi Host diganti
        // menjadi api.ice-stream.localhost. Target memakai 127.0.0.1 karena
        // Node.js tidak otomatis mengarahkan *.localhost ke 127.0.0.1
        // (curl dan Chrome melakukannya).
        '/api': {
          target,
          headers: { host },
        },
      },
    },
    test: {
      environment: 'jsdom',
      setupFiles: ['./src/test/setup.ts'],
      restoreMocks: true,
      // npm run test:coverage -> coverage/lcov.info, dibaca SonarQube
      // (sonar.javascript.lcov.reportPaths).
      coverage: {
        provider: 'v8',
        reporter: ['text-summary', 'lcov'],
        include: ['src/**/*.{ts,tsx}'],
        exclude: ['src/**/*.d.ts', 'src/**/*.test.{ts,tsx}', 'src/test/**', 'src/main.tsx', 'src/vite-env.d.ts'],
      },
    },
  }
})
