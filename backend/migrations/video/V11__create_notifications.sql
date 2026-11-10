-- Langkah 16: notifikasi video baru.
--
-- Satu baris = satu notifikasi untuk satu user (user_id). Untuk sekarang
-- hanya ada satu jenis (kind): NEW_VIDEO, "channel yang kamu subscribe
-- merilis video baru". Kolom kind dan actor_id disiapkan supaya jenis lain
-- (misalnya balasan komentar) bisa ditambah tanpa tabel baru.
--
-- Notifikasi dibuat saat video selesai diproses (READY): satu baris untuk
-- setiap subscriber channel itu ("fan-out on write"). Dengan begitu daftar
-- notifikasi dan jumlah yang belum dibaca cukup dibaca dari tabel ini.
--
-- user_id dan actor_id tanpa FOREIGN KEY karena tabel users ada di schema
-- AUTH_SVC (sama seperti watch_history.user_id).
CREATE TABLE notifications (
    id         NUMBER(19)               GENERATED ALWAYS AS IDENTITY,
    -- Penerima notifikasi.
    user_id    NUMBER(19)               NOT NULL,
    kind       VARCHAR2(20)             NOT NULL,
    video_id   NUMBER(19)               NOT NULL,
    -- Yang menyebabkan notifikasi: untuk NEW_VIDEO, pemilik channel.
    actor_id   NUMBER(19)               NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT SYSTIMESTAMP NOT NULL,
    -- NULL = belum dibaca.
    read_at    TIMESTAMP WITH TIME ZONE,
    CONSTRAINT notifications_pk       PRIMARY KEY (id),
    -- Satu user hanya mendapat satu notifikasi NEW_VIDEO untuk video yang
    -- sama, walau event-nya terkirim dua kali.
    CONSTRAINT notifications_once_uq  UNIQUE (user_id, video_id, kind),
    CONSTRAINT notifications_video_fk FOREIGN KEY (video_id) REFERENCES videos (id),
    CONSTRAINT notifications_kind_ck  CHECK (kind IN ('NEW_VIDEO'))
);

-- Halaman notifikasi, terbaru lebih dulu:
--   WHERE user_id = ? AND id < ? ORDER BY id DESC FETCH NEXT 21 ROWS ONLY
-- Diurutkan dengan id (bukan created_at), sama seperti komentar, jadi tidak
-- perlu SYS_EXTRACT_UTC seperti di riwayat tonton.
CREATE INDEX notifications_user_ix ON notifications (user_id, id);

-- Jumlah yang belum dibaca (badge lonceng):
--   WHERE user_id = ? AND SYS_EXTRACT_UTC(read_at) IS NULL
-- Index ini berisi baris dengan read_at NULL juga, karena user_id tidak
-- pernah NULL. Jadi Oracle langsung melompat ke notifikasi yang belum
-- dibaca, tanpa membaca yang sudah dibaca. video_id ikut disimpan di index
-- supaya Oracle tidak perlu membuka tabel notifications sama sekali.
--
-- Kenapa SYS_EXTRACT_UTC? Sama seperti di V9: kolom TIMESTAMP WITH TIME
-- ZONE disimpan di index sebagai SYS_EXTRACT_UTC(read_at). Query yang
-- menulis "read_at IS NULL" saja tidak bisa memakai bagian index itu
-- (dicek dengan EXPLAIN PLAN: 324 buffer jadi 96 untuk 82 notifikasi).
CREATE INDEX notifications_unread_ix ON notifications (user_id, read_at, video_id);

-- Foreign key ke videos butuh index di video_id (lihat V9).
CREATE INDEX notifications_video_ix ON notifications (video_id);
