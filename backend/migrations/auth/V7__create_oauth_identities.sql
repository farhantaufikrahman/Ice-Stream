-- Langkah 26: "Masuk dengan Google / GitHub" (desain Auth).
--
-- 1. User yang mendaftar lewat Google/GitHub tidak punya kata sandi. Kolom
--    password_hash boleh NULL; login dengan kata sandi untuk user seperti
--    ini selalu ditolak. Kata sandi bisa dibuat belakangan lewat
--    "Lupa kata sandi?" (langkah 24c).
ALTER TABLE users MODIFY (password_hash NULL);

-- 2. Akun Google/GitHub yang terhubung ke user Ice-Stream. subject adalah
--    id user di penyedia itu ("sub" di Google, "id" di GitHub), BUKAN
--    email: email bisa diganti, id tidak.
CREATE TABLE user_identities (
    id          NUMBER(19)               GENERATED ALWAYS AS IDENTITY,
    user_id     NUMBER(19)               NOT NULL,
    provider    VARCHAR2(10 CHAR)        NOT NULL,
    subject     VARCHAR2(100 CHAR)       NOT NULL,
    -- Email dari penyedia saat terakhir login, hanya untuk ditampilkan.
    email       VARCHAR2(254 CHAR),
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL,
    last_login_at TIMESTAMP WITH TIME ZONE NOT NULL,
    CONSTRAINT user_identities_pk          PRIMARY KEY (id),
    CONSTRAINT user_identities_provider_ck CHECK (provider IN ('google', 'github')),
    CONSTRAINT user_identities_subject_uq  UNIQUE (provider, subject),
    -- Satu user paling banyak punya satu akun per penyedia.
    CONSTRAINT user_identities_user_uq     UNIQUE (user_id, provider),
    CONSTRAINT user_identities_user_fk     FOREIGN KEY (user_id) REFERENCES users (id)
);

-- 3. Kode login sekali pakai. Setelah Google/GitHub mengembalikan user ke
--    gateway, gateway TIDAK langsung memasang cookie refresh token, karena
--    alamat callback bisa berbeda dengan alamat aplikasi (Google hanya
--    menerima http://localhost, bukan http://ice-stream.localhost). Gateway
--    membuat kode ini, mengarahkan browser ke aplikasi, lalu aplikasi
--    menukar kode menjadi token. Seperti token reset (V6), yang disimpan
--    hanya hash SHA-256-nya.
CREATE TABLE oauth_login_codes (
    id          NUMBER(19)               GENERATED ALWAYS AS IDENTITY,
    user_id     NUMBER(19)               NOT NULL,
    code_hash   VARCHAR2(64 CHAR)        NOT NULL,
    expires_at  TIMESTAMP WITH TIME ZONE NOT NULL,
    used_at     TIMESTAMP WITH TIME ZONE,
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL,
    CONSTRAINT oauth_login_codes_pk      PRIMARY KEY (id),
    CONSTRAINT oauth_login_codes_hash_uq UNIQUE (code_hash),
    CONSTRAINT oauth_login_codes_user_fk FOREIGN KEY (user_id) REFERENCES users (id)
);

CREATE INDEX oauth_login_codes_user_ix ON oauth_login_codes (user_id);
