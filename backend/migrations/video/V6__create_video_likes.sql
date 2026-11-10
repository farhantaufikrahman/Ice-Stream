-- Langkah 10: like video.
--
-- Satu baris = "user_id menyukai video_id". Primary key gabungan
-- (video_id, user_id) sekaligus menjamin satu user hanya bisa like satu
-- video sekali, dan menjadi index untuk "apakah user ini sudah like?".
-- user_id tanpa FOREIGN KEY karena tabel users ada di schema AUTH_SVC.
CREATE TABLE video_likes (
    video_id   NUMBER(19)               NOT NULL,
    user_id    NUMBER(19)               NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT SYSTIMESTAMP NOT NULL,
    CONSTRAINT video_likes_pk       PRIMARY KEY (video_id, user_id),
    CONSTRAINT video_likes_video_fk FOREIGN KEY (video_id) REFERENCES videos (id)
);

ALTER TABLE videos ADD (
    like_count NUMBER(19) DEFAULT 0 NOT NULL
);

ALTER TABLE videos ADD CONSTRAINT videos_like_count_ck CHECK (like_count >= 0);
