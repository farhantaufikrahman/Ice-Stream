-- Langkah 22b: tayangan per hari untuk dasbor Creator Studio.
--
-- videos.view_count hanya menyimpan total sepanjang masa. Grafik
-- "Penayangan per hari" butuh jumlah per tanggal, jadi setiap tayangan
-- yang dihitung (RecordView) juga menambah satu baris di sini.
--
-- Satu baris = jumlah tayangan satu video pada satu tanggal. Tabel ini
-- tumbuh paling banyak satu baris per video per hari, bukan satu baris per
-- tayangan, jadi tetap kecil walau videonya ditonton jutaan kali.
--
-- Tayangan sebelum migrasi ini tidak punya tanggal, jadi grafik dimulai
-- dari hari migrasi dijalankan.
CREATE TABLE video_daily_views (
    video_id  NUMBER(19) NOT NULL,
    -- Tanggal dalam WIB (UTC+7), tanpa jam. Dihitung di VideoService,
    -- bukan di Oracle, supaya tidak bergantung pada zona waktu database.
    view_date DATE       NOT NULL,
    views     NUMBER(19) DEFAULT 0 NOT NULL,
    -- Satu baris per video per tanggal. Primary key ini juga index untuk
    -- "tayangan video X dari tanggal A sampai B".
    CONSTRAINT video_daily_views_pk       PRIMARY KEY (video_id, view_date),
    -- Video dihapus secara soft delete (baris videos tetap ada), jadi
    -- foreign key ini tidak pernah menghalangi penghapusan video.
    CONSTRAINT video_daily_views_video_fk FOREIGN KEY (video_id) REFERENCES videos (id),
    CONSTRAINT video_daily_views_views_ck CHECK (views >= 0)
);

-- Dasbor channel menjumlahkan tayangan semua video milik satu user:
--   SELECT d.view_date, SUM(d.views) FROM video_daily_views d
--     JOIN videos v ON v.id = d.video_id
--    WHERE v.owner_id = ? AND d.view_date >= ? GROUP BY d.view_date
-- Oracle mengambil video milik user lewat videos_owner_id_ix lalu membaca
-- primary key di atas untuk setiap video, jadi tidak perlu index tambahan.

-- Dasbor juga menghitung danmaku dan komentar yang diterima dalam periode:
--   ... WHERE video_id IN (video milik user) AND created_at >= ?
-- Index lama (danmaku_video_pos_ix, comments_video_ix) tidak berisi
-- created_at, jadi Oracle harus membaca setiap baris tabelnya. Dengan dua
-- index ini jawabannya cukup dari index saja. deleted_at ikut disimpan
-- supaya komentar yang sudah dihapus bisa disaring tanpa membaca tabel.
CREATE INDEX danmaku_video_time_ix ON danmaku (video_id, created_at);
CREATE INDEX comments_video_time_ix ON comments (video_id, created_at, deleted_at);
