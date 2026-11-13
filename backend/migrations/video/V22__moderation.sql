-- Langkah 28: halaman moderasi admin.
--
-- Alurnya:
--   1. Penonton menekan "Laporkan" (langkah 24b, tabel video_reports).
--   2. Admin membuka antrean laporan OPEN, lalu memilih:
--      - "Turunkan video": video disembunyikan dari semua orang kecuali
--        pemiliknya, semua laporan OPEN video itu ditutup (BLOCKED), dan
--        pemilik mendapat notifikasi VIDEO_BLOCKED; atau
--      - "Tolak laporan": laporan OPEN ditutup (DISMISSED), video tetap.
--   3. Video yang diturunkan bisa dipulihkan admin. Visibilitasnya kembali
--      seperti sebelum diturunkan dan pemilik mendapat VIDEO_RESTORED.

-- 1. Visibilitas BLOCKED = "diturunkan moderator". Semua query publik
--    sudah menyaring visibility = 'PUBLIC' (atau IN ('PUBLIC', 'UNLISTED')),
--    jadi video BLOCKED otomatis hilang dari feed, pencarian, Trending,
--    rekomendasi, kanal, playlist, komentar, dan danmaku tanpa mengubah
--    query-query itu. Pemiliknya tetap bisa melihatnya di Studio, tapi
--    tidak bisa mengubah visibilitasnya.
ALTER TABLE videos DROP CONSTRAINT videos_visibility_ck;
ALTER TABLE videos ADD CONSTRAINT videos_visibility_ck
    CHECK (visibility IN ('PUBLIC', 'UNLISTED', 'PRIVATE', 'BLOCKED'));

-- 2. Catatan moderasi, satu baris per video. Baris tidak dihapus saat
--    video dipulihkan (restored_at diisi), supaya alasan penurunan tetap
--    bisa ditampilkan di notifikasi lama. Jika video yang sama diturunkan
--    lagi, barisnya ditimpa (MERGE).
--    previous_visibility: visibilitas sebelum diturunkan, dipakai saat
--    memulihkan.
--    moderator_id / restored_by tanpa FOREIGN KEY karena tabel users ada
--    di schema AUTH_SVC (sama seperti video_reports.reporter_id).
CREATE TABLE video_moderation (
    video_id            NUMBER(19)               NOT NULL,
    previous_visibility VARCHAR2(10 CHAR)        NOT NULL,
    reason              VARCHAR2(20 CHAR)        NOT NULL,
    note                VARCHAR2(500 CHAR),
    moderator_id        NUMBER(19)               NOT NULL,
    blocked_at          TIMESTAMP WITH TIME ZONE NOT NULL,
    restored_at         TIMESTAMP WITH TIME ZONE,
    restored_by         NUMBER(19),
    CONSTRAINT video_moderation_pk       PRIMARY KEY (video_id),
    CONSTRAINT video_moderation_video_fk FOREIGN KEY (video_id) REFERENCES videos (id),
    CONSTRAINT video_moderation_prev_ck  CHECK (previous_visibility IN ('PUBLIC', 'UNLISTED', 'PRIVATE')),
    -- Alasan sama dengan alasan laporan (video_reports_reason_ck).
    CONSTRAINT video_moderation_reason_ck CHECK (reason IN (
        'spam', 'sexual', 'violence', 'hate', 'harassment', 'misleading', 'copyright', 'other'))
);

-- Tab "Diturunkan" di halaman moderasi:
--   WHERE restored_at IS NULL ORDER BY blocked_at DESC
-- blocked_at selalu terisi, jadi setiap baris masuk index (Oracle tidak
-- menyimpan baris yang SEMUA kolom index-nya NULL).
CREATE INDEX video_moderation_active_ix ON video_moderation (restored_at, blocked_at);

-- 3. Hasil peninjauan laporan. status tetap OPEN/RESOLVED seperti V17;
--    resolution menjelaskan RESOLVED yang mana.
ALTER TABLE video_reports ADD (
    resolution  VARCHAR2(10 CHAR),
    resolved_at TIMESTAMP WITH TIME ZONE,
    resolved_by NUMBER(19),
    CONSTRAINT video_reports_resolution_ck CHECK (resolution IN ('BLOCKED', 'DISMISSED'))
);

-- 4. Dua jenis notifikasi baru untuk pemilik video. actor_id = admin.
ALTER TABLE notifications DROP CONSTRAINT notifications_kind_ck;
ALTER TABLE notifications ADD CONSTRAINT notifications_kind_ck
    CHECK (kind IN ('NEW_VIDEO', 'VIDEO_BLOCKED', 'VIDEO_RESTORED'));
