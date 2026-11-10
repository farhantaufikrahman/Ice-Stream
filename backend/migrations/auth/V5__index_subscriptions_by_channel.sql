-- Langkah 16: notifikasi video baru.
--
-- Saat sebuah video selesai diproses, VideoService meminta daftar
-- subscriber channel itu ke UserService, halaman demi halaman:
--   SELECT subscriber_id FROM subscriptions
--    WHERE channel_id = ? AND subscriber_id > ?
--    ORDER BY subscriber_id
--    FETCH NEXT 1000 ROWS ONLY
--
-- Index lama hanya berisi channel_id, jadi Oracle harus membaca SEMUA baris
-- subscriber channel itu dari tabel lalu mengurutkannya, untuk setiap
-- halaman. Index baru (channel_id, subscriber_id) sudah urut dan sudah
-- berisi kolom yang dibutuhkan, jadi Oracle cukup membaca index saja dan
-- berhenti setelah 1000 baris.
--
-- Index baru tetap diawali channel_id, jadi masih berfungsi sebagai index
-- FOREIGN KEY channel_id (alasan index lama dibuat, lihat V4).
DROP INDEX subscriptions_channel_ix;

CREATE INDEX subscriptions_channel_ix ON subscriptions (channel_id, subscriber_id);
