-- Langkah 24a: urutan "Danmaku terbanyak", tab "Terpopuler", dan Trending.
--
-- 1. Jumlah danmaku per video. Sama seperti comment_count (V5) dan
--    like_count (V6): disimpan di baris video dan ditambah di transaksi
--    yang sama dengan INSERT danmaku, supaya pencarian bisa mengurutkan
--    "Danmaku terbanyak" tanpa COUNT(*) ke tabel danmaku untuk setiap video.
ALTER TABLE videos ADD (
    danmaku_count NUMBER(19) DEFAULT 0 NOT NULL,
    CONSTRAINT videos_danmaku_count_ck CHECK (danmaku_count >= 0)
);

-- Isi jumlah untuk danmaku yang sudah ada sebelum migrasi ini.
MERGE INTO videos v
USING (SELECT video_id, COUNT(*) AS n FROM danmaku GROUP BY video_id) d
   ON (v.id = d.video_id)
 WHEN MATCHED THEN UPDATE SET v.danmaku_count = d.n;

-- 2. Tab "Terpopuler" di halaman channel:
--   WHERE owner_id = ? AND status = 'READY' ORDER BY view_count DESC, id DESC
-- Oracle membaca index ini dari belakang dan berhenti setelah satu halaman.
CREATE INDEX videos_owner_popular_ix ON videos (owner_id, status, view_count, id);

-- 3. Trending = tayangan 7 hari terakhir (video_daily_views, V14):
--   SELECT video_id, SUM(views) FROM video_daily_views
--    WHERE view_date >= ? GROUP BY video_id
-- Primary key tabel itu diawali video_id, jadi tidak cocok untuk "semua
-- video sejak tanggal X". Index ini diawali view_date dan juga berisi
-- views, jadi Oracle cukup membaca index tanpa membuka tabelnya.
CREATE INDEX video_daily_views_date_ix ON video_daily_views (view_date, video_id, views);
