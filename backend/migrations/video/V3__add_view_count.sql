-- Langkah 7: jumlah views dan index untuk feed publik.
--
-- DEFAULT 0 NOT NULL: video lama otomatis bernilai 0. Di Oracle 23ai,
-- menambah kolom dengan DEFAULT seperti ini hanya mengubah metadata tabel,
-- jadi cepat walau tabelnya besar.
ALTER TABLE videos ADD (
    view_count NUMBER(19) DEFAULT 0 NOT NULL
);

ALTER TABLE videos ADD CONSTRAINT videos_view_count_ck CHECK (view_count >= 0);

-- Feed publik: "video READY, yang paling baru selesai diproses lebih dulu".
-- Query feed selalu memfilter status lalu mengurutkan processed_at dan id,
-- jadi index berisi tiga kolom itu dengan urutan yang sama. Oracle bisa
-- membaca index ini dari belakang untuk ORDER BY ... DESC.
CREATE INDEX videos_feed_ix ON videos (status, processed_at, id);
