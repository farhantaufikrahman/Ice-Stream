-- Langkah 21b: Tonton nanti.
--
-- Satu baris = "user_id menyimpan video_id untuk ditonton nanti, pada
-- waktu added_at". Menyimpan video yang sama dua kali tidak menambah baris
-- (primary key gabungan), jadi daftarnya tidak pernah berisi duplikat.
--
-- user_id tanpa FOREIGN KEY karena tabel users ada di schema AUTH_SVC
-- (sama seperti watch_history.user_id).
CREATE TABLE watch_later (
    user_id  NUMBER(19)               NOT NULL,
    video_id NUMBER(19)               NOT NULL,
    -- Kapan video disimpan. Daftar Tonton nanti diurutkan dari yang
    -- terakhir disimpan.
    added_at TIMESTAMP WITH TIME ZONE DEFAULT SYSTIMESTAMP NOT NULL,
    -- Primary key gabungan menjamin satu baris per user per video, dan
    -- sekaligus menjadi index untuk "apakah user ini sudah menyimpan video X?"
    -- (tombol Simpan di halaman tonton).
    CONSTRAINT watch_later_pk       PRIMARY KEY (user_id, video_id),
    CONSTRAINT watch_later_video_fk FOREIGN KEY (video_id) REFERENCES videos (id)
);

-- Halaman Tonton nanti: video yang disimpan user, terbaru lebih dulu.
--   WHERE user_id = ?
--   ORDER BY SYS_EXTRACT_UTC(added_at) DESC, video_id DESC
--   FETCH NEXT 21 ROWS ONLY
-- Sama seperti watch_history_user_time_ix (V9): Oracle membaca index ini
-- dari belakang dan berhenti setelah 21 baris.
CREATE INDEX watch_later_user_time_ix ON watch_later (user_id, added_at, video_id);

-- Foreign key ke videos butuh index di video_id, supaya menghapus atau
-- mengubah baris videos tidak mengunci seluruh tabel watch_later.
CREATE INDEX watch_later_video_ix ON watch_later (video_id);
