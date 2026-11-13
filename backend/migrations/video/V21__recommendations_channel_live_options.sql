-- Langkah 27: rekomendasi Beranda, Video unggulan kanal, dan pilihan
-- siaran (rekaman, mode lambat, chat khusus pengikut).

-- 1. Kandidat "Rekomendasi untukmu": video publik paling banyak ditonton.
--      WHERE visibility = 'PUBLIC' AND draft = 0 AND status = 'READY'
--      ORDER BY view_count DESC, id DESC FETCH FIRST 200 ROWS ONLY
--    Sama seperti index feed (V20): kolom "=" di depan, kolom urutan di
--    belakang, jadi Oracle membaca index dari belakang lalu berhenti.
CREATE INDEX videos_popular_ix ON videos (visibility, draft, status, view_count, id);

-- 2. Pengaturan kanal yang datanya ada di VIDEO_SVC. Satu baris per
--    kreator, dibuat saat pertama kali diubah. featured_video_id = "Video
--    unggulan" di halaman kanal (desain Channel). ON DELETE tidak perlu:
--    video hanya di-soft delete, dan halaman kanal hanya menampilkan video
--    unggulan yang masih publik.
CREATE TABLE channel_settings (
    owner_id          NUMBER(19)               NOT NULL,
    featured_video_id NUMBER(19),
    updated_at        TIMESTAMP WITH TIME ZONE NOT NULL,
    CONSTRAINT channel_settings_pk       PRIMARY KEY (owner_id),
    CONSTRAINT channel_settings_video_fk FOREIGN KEY (featured_video_id) REFERENCES videos (id)
);

-- 3. Pilihan siaran (desain GoLive, "Detail siaran"):
--    record_enabled   "Simpan rekaman sebagai video setelah siaran selesai"
--    slow_mode        "Mode lambat chat (3 detik per pesan)". Default 1:
--                     sama dengan perilaku sejak langkah 25c.
--    followers_only   "Chat hanya untuk pengikut"
ALTER TABLE live_channels ADD (
    record_enabled NUMBER(1) DEFAULT 0 NOT NULL,
    slow_mode      NUMBER(1) DEFAULT 1 NOT NULL,
    followers_only NUMBER(1) DEFAULT 0 NOT NULL,
    CONSTRAINT live_channels_record_ck    CHECK (record_enabled IN (0, 1)),
    CONSTRAINT live_channels_slow_ck      CHECK (slow_mode IN (0, 1)),
    CONSTRAINT live_channels_followers_ck CHECK (followers_only IN (0, 1))
);

-- Mode lambat dan khusus pengikut dibaca langsung dari live_channels,
-- jadi mengubahnya saat live langsung berlaku. Rekaman dicatat per siaran:
--   recording_status: NULL = tidak direkam,
--                     PENDING = menunggu diimpor setelah siaran selesai,
--                     IMPORTING = sedang diimpor menjadi video,
--                     DONE = sudah menjadi video recording_video_id,
--                     FAILED = gagal (misalnya rekaman kosong).
--   recording_status diisi PENDING saat siaran selesai, jika pilihan
--   rekaman aktif pada saat itu.
--   recording_claimed_at: kapan pekerja mulai mengimpor. Jika pod mati di
--   tengah impor, baris IMPORTING yang sudah lama diambil lagi.
ALTER TABLE live_streams ADD (
    recording_status     VARCHAR2(10 CHAR),
    recording_video_id   NUMBER(19),
    recording_error      VARCHAR2(200 CHAR),
    recording_claimed_at TIMESTAMP WITH TIME ZONE,
    CONSTRAINT live_streams_recording_ck CHECK (recording_status IN ('PENDING', 'IMPORTING', 'DONE', 'FAILED')),
    CONSTRAINT live_streams_rec_video_fk FOREIGN KEY (recording_video_id) REFERENCES videos (id)
);

-- Pekerja impor rekaman mencari siaran yang menunggu:
--   WHERE recording_status IN ('PENDING', 'IMPORTING') ORDER BY ended_at
-- dan halaman Studio Live membaca rekaman terakhir satu kreator:
--   WHERE owner_id = ? AND recording_status IS NOT NULL ORDER BY ended_at DESC
-- (index live_streams_owner_ix dari V18 dipakai untuk yang kedua).
CREATE INDEX live_streams_recording_ix ON live_streams (recording_status, ended_at);
