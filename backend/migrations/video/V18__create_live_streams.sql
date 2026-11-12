-- Langkah 25a: siaran langsung (Live).
--
-- live_channels: pengaturan siaran milik satu kreator (satu baris per
-- user). Stream key hanya disimpan hash SHA-256-nya, sama seperti refresh
-- token di AuthService: jika database bocor, key aslinya tetap tidak bisa
-- dipakai untuk siaran atas nama kreator itu. Akibatnya key hanya bisa
-- dilihat sekali, saat dibuat.
--
-- owner_id tanpa FOREIGN KEY karena tabel users ada di schema AUTH_SVC
-- (sama seperti videos.owner_id).
CREATE TABLE live_channels (
    owner_id              NUMBER(19)               NOT NULL,
    -- Judul dan kategori untuk siaran berikutnya (dan siaran yang sedang
    -- berjalan, jika diubah saat live).
    title                 VARCHAR2(100 CHAR)       NOT NULL,
    category              VARCHAR2(20 CHAR),
    stream_key_hash       VARCHAR2(64 CHAR),
    stream_key_created_at TIMESTAMP WITH TIME ZONE,
    created_at            TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at            TIMESTAMP WITH TIME ZONE NOT NULL,
    CONSTRAINT live_channels_pk          PRIMARY KEY (owner_id),
    CONSTRAINT live_channels_key_uq      UNIQUE (stream_key_hash),
    -- Daftar yang sama dengan videos_category_ck (V15).
    CONSTRAINT live_channels_category_ck CHECK (category IN (
        'anime', 'game', 'musik', 'teknologi', 'kuliner', 'vlog', 'edukasi', 'olahraga'))
);

-- live_streams: satu baris = satu kali siaran, dari mulai sampai selesai.
-- Baris dibuat dan ditutup oleh VideoService berdasarkan status MediaMTX
-- (bukan oleh browser), jadi tetap benar walau OBS ditutup paksa.
CREATE TABLE live_streams (
    id           NUMBER(19)               GENERATED ALWAYS AS IDENTITY,
    owner_id     NUMBER(19)               NOT NULL,
    title        VARCHAR2(100 CHAR)       NOT NULL,
    category     VARCHAR2(20 CHAR),
    status       VARCHAR2(10 CHAR)        NOT NULL,
    started_at   TIMESTAMP WITH TIME ZONE NOT NULL,
    -- Terakhir kali MediaMTX melaporkan stream ini masih ada. Jika stream
    -- hilang sebentar (koneksi kreator putus lalu tersambung lagi), siaran
    -- yang sama dilanjutkan selama jaraknya masih di bawah batas tenggang.
    last_seen_at TIMESTAMP WITH TIME ZONE NOT NULL,
    ended_at     TIMESTAMP WITH TIME ZONE,
    -- Jumlah penonton menurut MediaMTX (satu sesi HLS = satu penonton),
    -- diperbarui setiap kali status dibaca. peak_viewers = angka tertinggi
    -- selama siaran.
    viewer_count NUMBER(10)               DEFAULT 0 NOT NULL,
    peak_viewers NUMBER(10)               DEFAULT 0 NOT NULL,
    CONSTRAINT live_streams_pk        PRIMARY KEY (id),
    CONSTRAINT live_streams_status_ck CHECK (status IN ('LIVE', 'ENDED')),
    CONSTRAINT live_streams_ended_ck  CHECK ((status = 'LIVE' AND ended_at IS NULL) OR (status = 'ENDED' AND ended_at IS NOT NULL)),
    CONSTRAINT live_streams_category_ck CHECK (category IN (
        'anime', 'game', 'musik', 'teknologi', 'kuliner', 'vlog', 'edukasi', 'olahraga'))
);

-- Satu kreator hanya boleh punya SATU siaran LIVE. Oracle tidak punya
-- "partial unique index" seperti PostgreSQL, jadi dipakai function-based
-- index: baris ENDED menghasilkan NULL, dan NULL tidak ikut dicek UNIQUE.
CREATE UNIQUE INDEX live_streams_one_live_ux ON live_streams (CASE WHEN status = 'LIVE' THEN owner_id END);

-- Beranda "Sedang live" (status LIVE, terbaru dulu) dan riwayat siaran
-- satu kreator.
CREATE INDEX live_streams_status_ix ON live_streams (status, started_at);
CREATE INDEX live_streams_owner_ix ON live_streams (owner_id, started_at);
