-- Membuat user (= schema) VIDEO_SVC untuk VideoService.
-- Dijalankan sebagai SYSTEM. ${video_svc_password} diisi Flyway dari
-- environment variable FLYWAY_PLACEHOLDERS_VIDEO_SVC_PASSWORD (lihat
-- infra/k8s/jobs/migrate-video.yaml), jadi password tidak ditulis di sini.
CREATE USER video_svc IDENTIFIED BY "${video_svc_password}"
    DEFAULT TABLESPACE users
    QUOTA UNLIMITED ON users;

-- DB_DEVELOPER_ROLE (Oracle 23ai) berisi hak membuat tabel, index, view,
-- sequence, dan sebagainya. Sama dengan yang diberikan ke AUTH_SVC.
GRANT DB_DEVELOPER_ROLE TO video_svc;