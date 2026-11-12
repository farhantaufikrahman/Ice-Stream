-- Langkah 24b: tombol "Laporkan" di halaman tonton (desain Watch).
--
-- Satu baris = "reporter_id melaporkan video_id". Primary key gabungan
-- (video_id, reporter_id): satu user hanya punya satu laporan per video.
-- Melapor lagi mengganti alasan dan keterangannya (bukan menambah baris),
-- jadi menekan tombol berkali-kali tidak bisa membanjiri antrean.
-- reporter_id tanpa FOREIGN KEY karena tabel users ada di schema AUTH_SVC
-- (sama seperti video_likes, V6).
CREATE TABLE video_reports (
    video_id    NUMBER(19)               NOT NULL,
    reporter_id NUMBER(19)               NOT NULL,
    reason      VARCHAR2(20 CHAR)        NOT NULL,
    details     VARCHAR2(500 CHAR),
    -- OPEN = belum ditinjau. Belum ada halaman moderasi; kolom ini
    -- disiapkan supaya nanti cukup UPDATE ke RESOLVED.
    status      VARCHAR2(10 CHAR)        DEFAULT 'OPEN' NOT NULL,
    reported_at TIMESTAMP WITH TIME ZONE NOT NULL,
    CONSTRAINT video_reports_pk       PRIMARY KEY (video_id, reporter_id),
    CONSTRAINT video_reports_video_fk FOREIGN KEY (video_id) REFERENCES videos (id),
    CONSTRAINT video_reports_reason_ck CHECK (reason IN (
        'spam', 'sexual', 'violence', 'hate', 'harassment', 'misleading', 'copyright', 'other')),
    CONSTRAINT video_reports_status_ck CHECK (status IN ('OPEN', 'RESOLVED'))
);

-- Antrean moderasi nanti: "laporan OPEN, terlama dulu".
CREATE INDEX video_reports_queue_ix ON video_reports (status, reported_at);
