-- Langkah 13: riwayat tonton.
--
-- Satu baris = "user_id pernah menonton video_id, terakhir sampai detik
-- position_ms, pada waktu watched_at". Menonton video yang sama lagi tidak
-- menambah baris baru, hanya memperbarui baris yang sudah ada. Jadi
-- riwayat tidak berisi video yang sama berkali-kali.
--
-- user_id tanpa FOREIGN KEY karena tabel users ada di schema AUTH_SVC
-- (sama seperti video_likes.user_id).
CREATE TABLE watch_history (
    user_id     NUMBER(19)               NOT NULL,
    video_id    NUMBER(19)               NOT NULL,
    -- Posisi terakhir yang ditonton, milidetik dari awal video. Dipakai
    -- untuk "lanjut menonton" dan garis progres di thumbnail.
    position_ms NUMBER(10)               DEFAULT 0 NOT NULL,
    -- Terakhir kali posisi ini dikirim player. Riwayat diurutkan dari
    -- yang paling baru.
    watched_at  TIMESTAMP WITH TIME ZONE DEFAULT SYSTIMESTAMP NOT NULL,
    -- Primary key gabungan menjamin satu baris per user per video, dan
    -- sekaligus menjadi index untuk "sampai mana user ini menonton video X?".
    CONSTRAINT watch_history_pk          PRIMARY KEY (user_id, video_id),
    CONSTRAINT watch_history_video_fk    FOREIGN KEY (video_id) REFERENCES videos (id),
    CONSTRAINT watch_history_position_ck CHECK (position_ms >= 0)
);

-- Halaman riwayat: video yang ditonton user, terbaru lebih dulu.
--   WHERE user_id = ?
--   ORDER BY SYS_EXTRACT_UTC(watched_at) DESC, video_id DESC
--   FETCH NEXT 21 ROWS ONLY
-- Oracle membaca index ini dari belakang (INDEX RANGE SCAN DESCENDING) dan
-- berhenti setelah 21 baris, tanpa mengurutkan semua riwayat user itu.
-- Index biasa (naik) sudah cukup; tidak perlu "watched_at DESC".
--
-- Kenapa SYS_EXTRACT_UTC? Untuk kolom TIMESTAMP WITH TIME ZONE, Oracle
-- menyimpan index-nya dalam waktu UTC, yaitu SYS_EXTRACT_UTC(watched_at).
-- Query harus mengurutkan dengan ekspresi yang sama persis supaya urutan
-- index bisa dipakai. Ini sudah dicek dengan EXPLAIN PLAN.
CREATE INDEX watch_history_user_time_ix ON watch_history (user_id, watched_at, video_id);

-- Foreign key ke videos butuh index di video_id. Tanpa index ini, setiap
-- kali baris videos diubah primary key-nya atau dihapus, Oracle mengunci
-- seluruh tabel watch_history.
CREATE INDEX watch_history_video_ix ON watch_history (video_id);
