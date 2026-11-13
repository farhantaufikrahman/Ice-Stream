-- Langkah 29: upload video multipart.
--
-- Cloudflare (paket gratis) menolak request di atas 100 MB, jadi file
-- video tidak lagi dikirim dengan satu PUT. Browser mengirimnya per bagian
-- 50 MB (S3 multipart upload), lalu VideoService menggabungkannya saat
-- CompleteUpload. upload_id adalah id multipart upload dari storage, diisi
-- saat CreateUpload. NULL untuk video lama (satu PUT) dan rekaman siaran.
-- https://docs.aws.amazon.com/AmazonS3/latest/userguide/mpuoverview.html
ALTER TABLE videos ADD (upload_id VARCHAR2(1024));
