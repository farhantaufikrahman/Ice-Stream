-- Langkah 15: edit dan hapus video.
--
-- Menghapus video memakai "soft delete": baris videos tidak dibuang, hanya
-- kolom deleted_at (sudah ada sejak V1) diisi. GORM otomatis menyaring baris
-- yang deleted_at-nya terisi, jadi video itu langsung hilang dari feed,
-- pencarian, channel, riwayat, komentar, dan danmaku. Baris komentar, like,
-- danmaku, dan riwayat yang menunjuk ke video itu tetap sah (FOREIGN KEY
-- tidak rusak).
--
-- File video di storage (raw/... dan hls/<id>/...) dihapus setelah baris
-- ditandai. Kolom ini mencatat kapan file-file itu selesai dihapus. NULL
-- pada video yang sudah dihapus = file-nya belum terhapus (misalnya storage
-- sedang mati), dan VideoService mencoba lagi saat start berikutnya.
ALTER TABLE videos ADD (
    media_purged_at TIMESTAMP WITH TIME ZONE
);
