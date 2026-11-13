-- Langkah 11: danmaku (komentar melayang di atas video).
--
-- Berbeda dengan komentar biasa, setiap danmaku menempel pada detik
-- tertentu di video (position_ms). Saat video diputar sampai detik itu,
-- player menampilkannya melayang di atas gambar.
--
-- user_id adalah id user di AUTH_SVC.USERS, tanpa FOREIGN KEY (sama seperti
-- videos.owner_id). Siapa pengirimnya tidak ditampilkan ke penonton lain,
-- tapi tetap disimpan untuk moderasi dan batas spam.
CREATE TABLE danmaku (
    id          NUMBER(19)               GENERATED ALWAYS AS IDENTITY,
    video_id    NUMBER(19)               NOT NULL,
    user_id     NUMBER(19)               NOT NULL,
    -- Detik ke berapa danmaku muncul, dalam milidetik dari awal video.
    position_ms NUMBER(10)               NOT NULL,
    -- Danmaku pendek dan satu baris: maksimal 100 huruf.
    body        VARCHAR2(100 CHAR)       NOT NULL,
    -- Warna teks sebagai angka RGB, 0 (#000000) sampai 16777215 (#FFFFFF).
    color       NUMBER(8)                DEFAULT 16777215 NOT NULL,
    -- SCROLL = melayang dari kanan ke kiri, TOP/BOTTOM = diam di atas/bawah.
    -- Namanya bukan "mode" karena MODE adalah kata khusus (reserved word)
    -- di Oracle 23ai dan tidak boleh dipakai sebagai nama kolom.
    display_mode VARCHAR2(6 CHAR)        DEFAULT 'SCROLL' NOT NULL,
    created_at  TIMESTAMP WITH TIME ZONE DEFAULT SYSTIMESTAMP NOT NULL,
    CONSTRAINT danmaku_pk          PRIMARY KEY (id),
    CONSTRAINT danmaku_video_fk    FOREIGN KEY (video_id) REFERENCES videos (id),
    CONSTRAINT danmaku_position_ck CHECK (position_ms >= 0),
    CONSTRAINT danmaku_color_ck    CHECK (color BETWEEN 0 AND 16777215),
    CONSTRAINT danmaku_mode_ck     CHECK (display_mode IN ('SCROLL', 'TOP', 'BOTTOM'))
);

-- Player mengambil danmaku per potongan waktu, misalnya menit 0 sampai 6:
--   WHERE video_id = ? AND position_ms BETWEEN ? AND ? ORDER BY position_ms, id
-- Index ini membuat query tersebut cukup membaca satu rentang index.
CREATE INDEX danmaku_video_pos_ix ON danmaku (video_id, position_ms, id);

-- Batas spam: "berapa danmaku yang dikirim user ini dalam 10 detik terakhir".
--   WHERE user_id = ? AND created_at > ?
CREATE INDEX danmaku_user_time_ix ON danmaku (user_id, created_at);
