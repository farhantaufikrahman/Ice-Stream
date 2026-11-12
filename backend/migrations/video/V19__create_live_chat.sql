-- Langkah 25b dan 25c: siaran dari browser dan chat siaran langsung.
--
-- 1. Token siaran browser (langkah 25b).
--
-- Siaran dari browser (WebRTC/WHIP) tidak memakai stream key, karena key
-- hanya ditampilkan sekali dan tidak boleh disimpan di browser. Sebagai
-- gantinya Studio meminta token pendek (berlaku beberapa menit) setiap kali
-- tombol "Mulai siaran" ditekan. Seperti stream key, yang disimpan hanya
-- hash SHA-256-nya. Satu kreator hanya punya satu token: token baru
-- menggantikan yang lama.
ALTER TABLE live_channels ADD (
    publish_token_hash       VARCHAR2(64 CHAR),
    publish_token_expires_at TIMESTAMP WITH TIME ZONE
);

-- 2. Chat siaran langsung (langkah 25c).
--
-- Berbeda dengan danmaku video (V7), pesan chat menempel pada waktu nyata,
-- bukan detik video, dan nama pengirimnya ditampilkan (seperti chat live
-- di Bilibili). user_id tanpa FOREIGN KEY karena tabel users ada di schema
-- AUTH_SVC.
CREATE TABLE live_chat_messages (
    id         NUMBER(19)               GENERATED ALWAYS AS IDENTITY,
    stream_id  NUMBER(19)               NOT NULL,
    user_id    NUMBER(19)               NOT NULL,
    -- Satu baris, maksimal 100 huruf (aturan yang sama dengan danmaku).
    body       VARCHAR2(100 CHAR)       NOT NULL,
    -- Warna teks saat tampil melayang di atas video, 0 sampai 0xFFFFFF.
    color      NUMBER(8)                DEFAULT 16777215 NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    CONSTRAINT live_chat_messages_pk       PRIMARY KEY (id),
    CONSTRAINT live_chat_messages_stream_fk FOREIGN KEY (stream_id) REFERENCES live_streams (id),
    CONSTRAINT live_chat_messages_color_ck CHECK (color BETWEEN 0 AND 16777215)
);

-- Penonton yang baru masuk mengambil pesan terakhir sebuah siaran:
--   WHERE stream_id = ? ORDER BY id DESC FETCH FIRST 50 ROWS ONLY
CREATE INDEX live_chat_stream_ix ON live_chat_messages (stream_id, id);

-- Mode lambat: "kapan user ini terakhir mengirim pesan".
--   WHERE user_id = ? AND created_at > ?
CREATE INDEX live_chat_user_time_ix ON live_chat_messages (user_id, created_at);
