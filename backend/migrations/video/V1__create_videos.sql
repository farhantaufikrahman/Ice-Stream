-- Tabel video milik VideoService (schema VIDEO_SVC).
-- owner_id adalah id user di AUTH_SVC.USERS. Sengaja tanpa FOREIGN KEY:
-- setiap service memiliki datanya sendiri, dan VideoService tidak boleh
-- bergantung langsung pada tabel milik AuthService.
CREATE TABLE videos (
    id           NUMBER(19)               GENERATED ALWAYS AS IDENTITY,
    owner_id     NUMBER(19)               NOT NULL,
    title        VARCHAR2(100 CHAR)       NOT NULL,
    -- 1000 CHAR: aman untuk huruf apa pun (maks 4 byte per huruf = 4000 byte,
    -- batas VARCHAR2 di Oracle).
    description  VARCHAR2(1000 CHAR),
    status       VARCHAR2(20 CHAR)        DEFAULT 'PENDING_UPLOAD' NOT NULL,
    -- Lokasi file di bucket, misalnya "raw/3f9c.../source".
    object_key   VARCHAR2(200 CHAR)       NOT NULL,
    content_type VARCHAR2(100 CHAR)       NOT NULL,
    size_bytes   NUMBER(19)               NOT NULL,
    uploaded_at  TIMESTAMP WITH TIME ZONE,
    created_at   TIMESTAMP WITH TIME ZONE DEFAULT SYSTIMESTAMP NOT NULL,
    updated_at   TIMESTAMP WITH TIME ZONE DEFAULT SYSTIMESTAMP NOT NULL,
    deleted_at   TIMESTAMP WITH TIME ZONE,
    CONSTRAINT videos_pk            PRIMARY KEY (id),
    CONSTRAINT videos_object_key_uq UNIQUE (object_key),
    CONSTRAINT videos_status_ck     CHECK (status IN ('PENDING_UPLOAD', 'UPLOADED')),
    CONSTRAINT videos_size_ck       CHECK (size_bytes > 0)
);

-- Untuk "video milik saya", diurutkan dari yang terbaru.
CREATE INDEX videos_owner_id_ix ON videos (owner_id, id DESC);