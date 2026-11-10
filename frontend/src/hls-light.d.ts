// Paket hls.js tidak menyertakan file tipe untuk "hls.js/light", padahal
// isinya sama dengan versi lengkap (hanya fiturnya lebih sedikit). Pinjam
// tipe dari "hls.js".
declare module 'hls.js/light' {
  export * from 'hls.js'
  export { default } from 'hls.js'
}
