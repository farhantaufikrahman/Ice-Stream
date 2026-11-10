-- Langkah 8: avatar disimpan sebagai lokasi file di bucket (key), misalnya
-- "avatars/7/3f9c0a1b.jpg", bukan URL lengkap. URL lengkap disusun saat
-- dibaca dari MEDIA_PUBLIC_URL, sama seperti hls_url di VideoService, jadi
-- pindah ke CDN cukup dengan mengganti config tanpa mengubah data.
-- Kolom ini belum pernah diisi (selalu NULL), jadi aman diganti nama.
ALTER TABLE users RENAME COLUMN avatar_url TO avatar_key;
