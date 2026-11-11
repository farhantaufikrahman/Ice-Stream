-- Langkah 21c: Playlist.
--
-- Satu playlist dimiliki satu user dan berisi banyak video. Video yang
-- sama boleh ada di banyak playlist, tapi hanya sekali per playlist.
--
-- owner_id tanpa FOREIGN KEY karena tabel users ada di schema AUTH_SVC
-- (sama seperti videos.owner_id).
CREATE TABLE playlists (
    id          NUMBER(19)               GENERATED ALWAYS AS IDENTITY,
    owner_id    NUMBER(19)               NOT NULL,
    -- Aturan panjangnya sama dengan judul dan deskripsi video.
    title       VARCHAR2(100 CHAR)       NOT NULL,
    description VARCHAR2(1000 CHAR),
    -- PUBLIC: terlihat di halaman kanal dan bisa dibuka siapa saja.
    -- PRIVATE: hanya pemiliknya yang bisa melihat.
    visibility  VARCHAR2(10)             DEFAULT 'PRIVATE' NOT NULL,
    created_at  TIMESTAMP WITH TIME ZONE DEFAULT SYSTIMESTAMP NOT NULL,
    -- Diperbarui saat judul/deskripsi/visibilitas diubah dan saat video
    -- ditambah atau dikeluarkan.
    updated_at  TIMESTAMP WITH TIME ZONE DEFAULT SYSTIMESTAMP NOT NULL,
    CONSTRAINT playlists_pk            PRIMARY KEY (id),
    CONSTRAINT playlists_visibility_ck CHECK (visibility IN ('PUBLIC', 'PRIVATE'))
);

-- Daftar playlist satu user (tab Playlist di kanal, dialog Simpan):
--   WHERE owner_id = ? [AND visibility = 'PUBLIC'] ORDER BY id DESC
-- Juga dipakai untuk menghitung jumlah playlist user (batas per akun).
CREATE INDEX playlists_owner_ix ON playlists (owner_id, id);

CREATE TABLE playlist_items (
    playlist_id NUMBER(19)               NOT NULL,
    video_id    NUMBER(19)               NOT NULL,
    -- Urutan video di playlist: yang lebih dulu ditambahkan tampil di atas.
    added_at    TIMESTAMP WITH TIME ZONE DEFAULT SYSTIMESTAMP NOT NULL,
    -- Satu video hanya sekali per playlist.
    CONSTRAINT playlist_items_pk          PRIMARY KEY (playlist_id, video_id),
    -- ON DELETE CASCADE: menghapus playlist otomatis menghapus isinya.
    CONSTRAINT playlist_items_playlist_fk FOREIGN KEY (playlist_id) REFERENCES playlists (id) ON DELETE CASCADE,
    CONSTRAINT playlist_items_video_fk    FOREIGN KEY (video_id) REFERENCES videos (id)
);

-- Isi satu playlist secara berurutan:
--   WHERE playlist_id = ? ORDER BY SYS_EXTRACT_UTC(added_at), video_id
CREATE INDEX playlist_items_order_ix ON playlist_items (playlist_id, added_at, video_id);

-- Dua kegunaan: index untuk foreign key ke videos (supaya mengubah baris
-- videos tidak mengunci seluruh tabel ini), dan pertanyaan "apakah video X
-- ada di salah satu playlist user ini?" di halaman tonton.
CREATE INDEX playlist_items_video_ix ON playlist_items (video_id);
