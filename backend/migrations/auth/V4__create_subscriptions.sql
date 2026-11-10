-- Langkah 10: subscribe channel.
--
-- Satu baris = "subscriber_id mengikuti channel_id". Keduanya id user di
-- tabel users (schema yang sama), jadi memakai FOREIGN KEY.
-- id dipakai untuk urutan dan pagination daftar subscription (terbaru
-- lebih dulu), sama seperti id komentar di VideoService.
CREATE TABLE subscriptions (
    id            NUMBER(19)               GENERATED ALWAYS AS IDENTITY,
    subscriber_id NUMBER(19)               NOT NULL,
    channel_id    NUMBER(19)               NOT NULL,
    created_at    TIMESTAMP WITH TIME ZONE DEFAULT SYSTIMESTAMP NOT NULL,
    CONSTRAINT subscriptions_pk            PRIMARY KEY (id),
    -- Satu user hanya bisa subscribe satu channel sekali.
    CONSTRAINT subscriptions_pair_uq       UNIQUE (subscriber_id, channel_id),
    CONSTRAINT subscriptions_subscriber_fk FOREIGN KEY (subscriber_id) REFERENCES users (id),
    CONSTRAINT subscriptions_channel_fk    FOREIGN KEY (channel_id) REFERENCES users (id),
    CONSTRAINT subscriptions_self_ck       CHECK (subscriber_id <> channel_id)
);

-- Daftar "channel yang saya subscribe", terbaru lebih dulu:
--   WHERE subscriber_id = ? AND id < ? ORDER BY id DESC
CREATE INDEX subscriptions_subscriber_ix ON subscriptions (subscriber_id, id);

-- Kolom FOREIGN KEY channel_id diberi index juga. Tanpa index ini, Oracle
-- mengunci seluruh tabel subscriptions setiap kali baris users diubah
-- primary key-nya atau dihapus.
CREATE INDEX subscriptions_channel_ix ON subscriptions (channel_id);

-- Jumlah subscriber, supaya halaman channel tidak perlu COUNT(*).
ALTER TABLE users ADD (
    subscriber_count NUMBER(19) DEFAULT 0 NOT NULL
);

ALTER TABLE users ADD CONSTRAINT users_subscriber_count_ck CHECK (subscriber_count >= 0);
