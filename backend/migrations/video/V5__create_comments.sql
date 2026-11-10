-- Langkah 9: komentar video.
--
-- Komentar dibuat dua tingkat, sama seperti YouTube dan Bilibili:
--   - komentar utama: parent_id NULL;
--   - balasan: parent_id = id komentar utama. Balasan untuk balasan tetap
--     disimpan di bawah komentar utama yang sama (tidak bertingkat terus).
--
-- author_id adalah id user di AUTH_SVC.USERS, tanpa FOREIGN KEY (sama
-- seperti videos.owner_id). video_id dan parent_id memakai FOREIGN KEY
-- karena tabelnya ada di schema yang sama (VIDEO_SVC).
CREATE TABLE comments (
    id          NUMBER(19)               GENERATED ALWAYS AS IDENTITY,
    video_id    NUMBER(19)               NOT NULL,
    author_id   NUMBER(19)               NOT NULL,
    parent_id   NUMBER(19),
    -- 1000 CHAR: maksimal 4 byte per huruf = 4000 byte, batas VARCHAR2.
    body        VARCHAR2(1000 CHAR)      NOT NULL,
    -- Jumlah balasan yang belum dihapus. Hanya dipakai komentar utama.
    reply_count NUMBER(19)               DEFAULT 0 NOT NULL,
    created_at  TIMESTAMP WITH TIME ZONE DEFAULT SYSTIMESTAMP NOT NULL,
    -- Komentar yang dihapus tidak benar-benar dibuang (soft delete), supaya
    -- masih bisa diperiksa jika ada laporan penyalahgunaan.
    deleted_at  TIMESTAMP WITH TIME ZONE,
    CONSTRAINT comments_pk          PRIMARY KEY (id),
    CONSTRAINT comments_video_fk    FOREIGN KEY (video_id) REFERENCES videos (id),
    CONSTRAINT comments_parent_fk   FOREIGN KEY (parent_id) REFERENCES comments (id),
    CONSTRAINT comments_reply_ck    CHECK (reply_count >= 0)
);

-- Satu index untuk dua query:
--   komentar utama: WHERE video_id = ? AND parent_id IS NULL ORDER BY id DESC
--   balasan:        WHERE video_id = ? AND parent_id = ?     ORDER BY id
-- Baris dengan parent_id NULL tetap masuk index ini karena video_id tidak
-- pernah NULL (Oracle hanya melewatkan baris yang SEMUA kolom index-nya NULL).
CREATE INDEX comments_video_ix ON comments (video_id, parent_id, id);

-- Jumlah komentar (utama + balasan) yang belum dihapus, untuk ditampilkan
-- di halaman video tanpa harus menghitung COUNT(*) setiap kali.
ALTER TABLE videos ADD (
    comment_count NUMBER(19) DEFAULT 0 NOT NULL
);

ALTER TABLE videos ADD CONSTRAINT videos_comment_count_ck CHECK (comment_count >= 0);
