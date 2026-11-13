-- Langkah 6a: status dan kolom untuk hasil transcoding (HLS).
--
-- Alur status:
--   PENDING_UPLOAD -> UPLOADED -> PROCESSING -> READY
--                                           \-> FAILED
-- File V1 tidak diubah (Flyway mencatat checksum-nya), jadi perubahan
-- tabel dibuat di file baru ini.

ALTER TABLE videos DROP CONSTRAINT videos_status_ck;

ALTER TABLE videos ADD CONSTRAINT videos_status_ck
    CHECK (status IN ('PENDING_UPLOAD', 'UPLOADED', 'PROCESSING', 'READY', 'FAILED'));

ALTER TABLE videos ADD (
    -- Lokasi master playlist HLS di bucket, misalnya "hls/42/master.m3u8".
    playlist_key   VARCHAR2(200 CHAR),
    -- Lokasi thumbnail di bucket, misalnya "hls/42/thumbnail.jpg".
    thumbnail_key  VARCHAR2(200 CHAR),
    duration_ms    NUMBER(19),
    -- Resolusi yang tersedia, dipisah koma, misalnya "360p,720p".
    renditions     VARCHAR2(100 CHAR),
    -- Alasan singkat jika status FAILED.
    failure_reason VARCHAR2(500 CHAR),
    -- Waktu transcoding selesai (READY atau FAILED).
    processed_at   TIMESTAMP WITH TIME ZONE
);