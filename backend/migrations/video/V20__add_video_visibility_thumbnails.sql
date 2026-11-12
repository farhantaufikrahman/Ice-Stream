-- Langkah 26: visibilitas, draf, bahasa video, dan pilihan thumbnail
-- (desain Upload: "Visibilitas & terbitkan", "Bahasa video", "Thumbnail").
--
-- visibility:
--   PUBLIC   = tampil di feed, pencarian, Trending, dan halaman kanal.
--   UNLISTED = "Tidak publik": hanya bisa dibuka lewat tautan.
--   PRIVATE  = "Pribadi": hanya pemiliknya.
-- draft = 1: kreator menekan "Simpan draf". Video draf hanya terlihat oleh
-- pemiliknya, apa pun visibilitasnya, sampai diterbitkan dari Studio.
--
-- published_at: waktu video pertama kali bisa ditonton orang lain (READY
-- dan bukan draf). Feed sekarang diurutkan dengan kolom ini, bukan
-- processed_at, supaya draf yang diterbitkan belakangan tampil sebagai
-- video baru.
ALTER TABLE videos ADD (
    visibility       VARCHAR2(10 CHAR) DEFAULT 'PUBLIC' NOT NULL,
    draft            NUMBER(1)         DEFAULT 0 NOT NULL,
    published_at     TIMESTAMP WITH TIME ZONE,
    -- Kode bahasa ISO 639-1. NULL = tidak diisi (video lama).
    language         VARCHAR2(5 CHAR),
    -- Posisi (milidetik) bingkai pilihan thumbnail dari transcoder, dipisah
    -- koma, misalnya "3000,64000,115000". NULL untuk video lama.
    thumbnail_frames VARCHAR2(100 CHAR),
    CONSTRAINT videos_visibility_ck CHECK (visibility IN ('PUBLIC', 'UNLISTED', 'PRIVATE')),
    CONSTRAINT videos_draft_ck      CHECK (draft IN (0, 1)),
    CONSTRAINT videos_language_ck   CHECK (language IN ('id', 'en', 'ja'))
);

-- Video lama sudah tampil di feed sejak processed_at.
UPDATE videos SET published_at = processed_at WHERE status = 'READY';

-- Index feed lama (V3, V4, V15, V16) disusun untuk "status = READY ORDER BY
-- processed_at". Syarat feed sekarang:
--   WHERE visibility = 'PUBLIC' AND draft = 0 AND status = 'READY'
--   ORDER BY published_at DESC, id DESC
-- Kolom yang dibandingkan dengan "=" di depan, kolom urutan di belakang,
-- jadi Oracle tetap membaca index dari belakang lalu berhenti setelah satu
-- halaman, sama seperti sebelumnya.
DROP INDEX videos_feed_ix;
DROP INDEX videos_owner_feed_ix;
DROP INDEX videos_category_feed_ix;
DROP INDEX videos_owner_popular_ix;

CREATE INDEX videos_public_feed_ix   ON videos (visibility, draft, status, published_at, id);
CREATE INDEX videos_owner_feed_ix    ON videos (owner_id, visibility, draft, status, published_at, id);
CREATE INDEX videos_category_feed_ix ON videos (category, visibility, draft, status, published_at, id);
CREATE INDEX videos_owner_popular_ix ON videos (owner_id, visibility, draft, status, view_count, id);
