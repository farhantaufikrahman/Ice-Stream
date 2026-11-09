# Image migrasi: Flyway + file SQL milik Ice-Stream.
# Setiap folder dipakai oleh Job yang berbeda (lihat infra/k8s/jobs/).
FROM flyway/flyway:11.20.3-alpine
COPY auth /flyway/migrations/auth
COPY bootstrap /flyway/migrations/bootstrap
COPY video /flyway/migrations/video