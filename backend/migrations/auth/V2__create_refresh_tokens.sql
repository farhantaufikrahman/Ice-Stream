-- Refresh token yang pernah diberikan. Yang disimpan hanya hash SHA-256-nya,
-- jadi kalau database bocor, token aslinya tetap tidak bisa dipakai.
CREATE TABLE refresh_tokens (
    id          NUMBER(19)               GENERATED ALWAYS AS IDENTITY,
    user_id     NUMBER(19)               NOT NULL,
    token_hash  VARCHAR2(64 CHAR)        NOT NULL,
    user_agent  VARCHAR2(255 CHAR),
    ip_address  VARCHAR2(45 CHAR),
    expires_at  TIMESTAMP WITH TIME ZONE NOT NULL,
    revoked_at  TIMESTAMP WITH TIME ZONE,
    created_at  TIMESTAMP WITH TIME ZONE DEFAULT SYSTIMESTAMP NOT NULL,
    CONSTRAINT refresh_tokens_pk            PRIMARY KEY (id),
    CONSTRAINT refresh_tokens_token_hash_uq UNIQUE (token_hash),
    CONSTRAINT refresh_tokens_user_fk       FOREIGN KEY (user_id) REFERENCES users (id)
);

-- Untuk mencabut semua token milik satu user sekaligus.
CREATE INDEX refresh_tokens_user_id_ix ON refresh_tokens (user_id);