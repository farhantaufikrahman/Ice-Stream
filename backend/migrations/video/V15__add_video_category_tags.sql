-- Langkah 23: kategori dan tag video.
--
-- Kategori: satu video punya paling banyak satu kategori dari daftar tetap
-- di bawah (sama dengan daftar di VideoService, lihat category.go). Video
-- lama tidak punya kategori (NULL) dan tetap tampil di "Semua".
--
-- Tag: kata bebas pilihan kreator, misalnya "esKrim anakKos". Disimpan
-- dipisah spasi di satu kolom, seperti kolom renditions ("360p,720p").
-- Tag hanya berisi huruf dan angka, jadi spasi aman sebagai pemisah.
-- Tag juga disimpan di video_search_terms (bobot judul), jadi mencari
-- "eskrim" menemukan video bertag #esKrim tanpa index tambahan.
ALTER TABLE videos ADD (
    category VARCHAR2(20 CHAR),
    -- Maksimal 10 tag x 30 huruf + 9 spasi = 309.
    tags     VARCHAR2(320 CHAR),
    CONSTRAINT videos_category_ck CHECK (category IN (
        'anime', 'game', 'musik', 'teknologi', 'kuliner', 'vlog', 'edukasi', 'olahraga'
    ))
);

-- Feed per kategori di beranda (chip "Anime", "Game", ...):
--   WHERE category = 'game' AND status = 'READY'
--   ORDER BY processed_at DESC, id DESC
-- Urutan kolomnya sama dengan videos_feed_ix (V3) dan videos_owner_feed_ix
-- (V4), ditambah category di depan, jadi Oracle bisa membaca index ini
-- dari belakang lalu berhenti setelah satu halaman.
CREATE INDEX videos_category_feed_ix ON videos (category, status, processed_at, id);
