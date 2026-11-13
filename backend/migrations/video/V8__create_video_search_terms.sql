-- Langkah 12: pencarian video.
--
-- Image Oracle yang dipakai (gvenzl/oracle-free:23-slim) tidak berisi
-- Oracle Text, jadi pencarian memakai "inverted index" buatan sendiri:
-- VideoService memecah judul dan deskripsi menjadi kata, lalu menyimpan
-- setiap kata di tabel ini.
--
--   term     video_id  weight
--   belajar  42        2       (ada di judul)
--   golang   42        2
--   pemula   42        1       (hanya di deskripsi)
--
-- ORGANIZATION INDEX (index-organized table): seluruh tabel disimpan
-- sebagai index B-tree urut (term, video_id). Pencarian "kata yang diawali
-- gol" cukup membaca satu potongan index itu, tanpa tabel terpisah.
CREATE TABLE video_search_terms (
    -- Kata yang sudah dinormalkan: huruf kecil, tanpa aksen, 2-30 huruf.
    term     VARCHAR2(30 CHAR) NOT NULL,
    video_id NUMBER(19)        NOT NULL,
    -- 2 = kata ada di judul, 1 = hanya di deskripsi.
    weight   NUMBER(1)         NOT NULL,
    CONSTRAINT video_search_terms_pk       PRIMARY KEY (term, video_id),
    CONSTRAINT video_search_terms_video_fk FOREIGN KEY (video_id) REFERENCES videos (id),
    CONSTRAINT video_search_terms_weight_ck CHECK (weight IN (1, 2))
) ORGANIZATION INDEX;

-- Untuk menghapus semua kata milik satu video (saat diindeks ulang).
CREATE INDEX video_search_terms_video_ix ON video_search_terms (video_id);

-- Waktu kata-kata video ini terakhir disimpan. NULL = belum diindeks.
-- Video yang sudah ada sebelum migrasi ini diindeks otomatis oleh
-- VideoService saat start (lihat Service.IndexPending).
ALTER TABLE videos ADD (
    search_indexed_at TIMESTAMP WITH TIME ZONE
);
