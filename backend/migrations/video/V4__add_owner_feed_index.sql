-- Langkah 8: index untuk daftar video di halaman channel, yaitu
-- "video READY milik user X, yang paling baru selesai diproses lebih dulu".
-- Urutan kolom sama dengan query: owner_id dan status difilter, lalu
-- processed_at dan id diurutkan.
CREATE INDEX videos_owner_feed_ix ON videos (owner_id, status, processed_at, id);
