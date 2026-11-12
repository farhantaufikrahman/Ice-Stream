-- Langkah 24c: "Lupa kata sandi?" (desain Auth).
--
-- Satu baris = satu tautan reset yang dikirim lewat email. Sama seperti
-- refresh_tokens (V2), yang disimpan hanya hash SHA-256 token-nya, jadi
-- isi database yang bocor tidak bisa dipakai untuk mengganti password.
--
-- Token hanya berlaku sekali (used_at) dan sebentar (expires_at). Meminta
-- tautan baru menandai tautan lama milik user yang sama sebagai terpakai.
CREATE TABLE password_reset_tokens (
    id          NUMBER(19)               GENERATED ALWAYS AS IDENTITY,
    user_id     NUMBER(19)               NOT NULL,
    token_hash  VARCHAR2(64 CHAR)        NOT NULL,
    -- Alamat IP yang meminta, untuk ditelusuri jika ada penyalahgunaan.
    request_ip  VARCHAR2(45 CHAR),
    expires_at  TIMESTAMP WITH TIME ZONE NOT NULL,
    used_at     TIMESTAMP WITH TIME ZONE,
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL,
    CONSTRAINT password_reset_tokens_pk            PRIMARY KEY (id),
    CONSTRAINT password_reset_tokens_token_hash_uq UNIQUE (token_hash),
    CONSTRAINT password_reset_tokens_user_fk       FOREIGN KEY (user_id) REFERENCES users (id)
);

-- Untuk "berapa tautan yang diminta user ini dalam 1 jam terakhir" dan
-- untuk menandai tautan lama sebagai terpakai.
CREATE INDEX password_reset_tokens_user_ix ON password_reset_tokens (user_id, created_at);
