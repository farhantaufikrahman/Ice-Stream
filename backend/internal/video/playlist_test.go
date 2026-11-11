package video

import (
	"cmp"
	"context"
	"encoding/base64"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Versi palsu method playlist di Repository. Perilakunya meniru query
// Oracle di playlist_repository.go.

func (r *fakeRepo) CreatePlaylist(_ context.Context, p NewPlaylist) (Playlist, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	now := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	pl := Playlist{ID: r.nextID, OwnerID: p.OwnerID, Title: p.Title, Description: p.Description,
		Visibility: p.Visibility, CreatedAt: now, UpdatedAt: now}
	r.playlists[pl.ID] = pl
	return pl, nil
}

func (r *fakeRepo) CountPlaylists(_ context.Context, ownerID int64) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int64
	for _, p := range r.playlists {
		if p.OwnerID == ownerID {
			n++
		}
	}
	return n, nil
}

func (r *fakeRepo) FindPlaylist(_ context.Context, id int64) (Playlist, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.playlists[id]
	if !ok {
		return Playlist{}, ErrPlaylistNotFound
	}
	return p, nil
}

func (r *fakeRepo) UpdatePlaylist(_ context.Context, p Playlist) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.playlists[p.ID]
	if !ok {
		return ErrPlaylistNotFound
	}
	old.Title, old.Description, old.Visibility, old.UpdatedAt = p.Title, p.Description, p.Visibility, p.UpdatedAt
	r.playlists[p.ID] = old
	return nil
}

func (r *fakeRepo) DeletePlaylist(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.playlists, id)
	delete(r.items, id) // ON DELETE CASCADE
	return nil
}

func (r *fakeRepo) ListPlaylists(_ context.Context, ownerID int64, includePrivate bool, afterID int64, limit int) ([]Playlist, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Playlist
	for _, p := range r.playlists {
		if p.OwnerID != ownerID || (!includePrivate && p.Visibility != VisibilityPublic) {
			continue
		}
		if afterID > 0 && p.ID >= afterID {
			continue
		}
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b Playlist) int { return cmp.Compare(b.ID, a.ID) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *fakeRepo) PlaylistStats(_ context.Context, ids []int64) (map[int64]PlaylistStats, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[int64]PlaylistStats{}
	for _, id := range ids {
		entries := r.readyItemsLocked(id)
		if len(entries) == 0 {
			continue
		}
		out[id] = PlaylistStats{VideoCount: int64(len(entries)), ThumbnailKey: entries[0].Video.ThumbnailKey}
	}
	return out, nil
}

func (r *fakeRepo) PlaylistsContaining(_ context.Context, ids []int64, videoID int64) (map[int64]bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[int64]bool{}
	for _, id := range ids {
		if _, ok := r.items[id][videoID]; ok {
			out[id] = true
		}
	}
	return out, nil
}

func (r *fakeRepo) IsInAnyPlaylist(_ context.Context, ownerID, videoID int64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, videos := range r.items {
		if _, ok := videos[videoID]; ok && r.playlists[id].OwnerID == ownerID {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeRepo) PlaylistItemAddedAt(_ context.Context, playlistID, videoID int64) (time.Time, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	at, ok := r.items[playlistID][videoID]
	return at, ok, nil
}

func (r *fakeRepo) CountPlaylistItems(_ context.Context, playlistID int64) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return int64(len(r.items[playlistID])), nil
}

func (r *fakeRepo) AddPlaylistItem(_ context.Context, playlistID, videoID int64, now time.Time) (time.Time, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.playlists[playlistID]
	if !ok {
		return time.Time{}, ErrPlaylistNotFound
	}
	if r.items[playlistID] == nil {
		r.items[playlistID] = map[int64]time.Time{}
	}
	if at, ok := r.items[playlistID][videoID]; ok {
		return at, nil
	}
	r.items[playlistID][videoID] = now
	p.UpdatedAt = now
	r.playlists[playlistID] = p
	return now, nil
}

func (r *fakeRepo) RemovePlaylistItem(_ context.Context, playlistID, videoID int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[playlistID][videoID]; !ok {
		return nil
	}
	delete(r.items[playlistID], videoID)
	p := r.playlists[playlistID]
	p.UpdatedAt = now
	r.playlists[playlistID] = p
	return nil
}

func (r *fakeRepo) ListPlaylistItems(_ context.Context, playlistID int64, after *PlaylistEntryCursor, limit int) ([]PlaylistEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []PlaylistEntry
	for _, e := range r.readyItemsLocked(playlistID) {
		if after != nil && !(e.AddedAt.After(after.AddedAt) || (e.AddedAt.Equal(after.AddedAt) && e.Video.ID > after.VideoID)) {
			continue
		}
		out = append(out, e)
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// readyItemsLocked: isi playlist untuk video READY, ORDER BY added_at,
// video_id. r.mu harus sudah dikunci.
func (r *fakeRepo) readyItemsLocked(playlistID int64) []PlaylistEntry {
	var out []PlaylistEntry
	for id, at := range r.items[playlistID] {
		if v, ok := r.videos[id]; ok && v.Status == StatusReady {
			out = append(out, PlaylistEntry{Video: v, AddedAt: at})
		}
	}
	slices.SortFunc(out, func(a, b PlaylistEntry) int {
		if c := a.AddedAt.Compare(b.AddedAt); c != 0 {
			return c
		}
		return cmp.Compare(a.Video.ID, b.Video.ID)
	})
	return out
}

func playlistIDs(list []Playlist) []int64 {
	out := make([]int64, len(list))
	for i, p := range list {
		out[i] = p.ID
	}
	return out
}

func entryIDs(entries []PlaylistEntry) []int64 {
	out := make([]int64, len(entries))
	for i, e := range entries {
		out[i] = e.Video.ID
	}
	return out
}

func TestCreatePlaylist(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService()

	p, err := svc.CreatePlaylist(ctx, CreatePlaylistInput{RequesterID: 8, Title: "  Belajar Go  ", Description: " dasar "})
	require.NoError(t, err)
	assert.Equal(t, "Belajar Go", p.Title, "spasi dibuang")
	assert.Equal(t, "dasar", p.Description)
	assert.Equal(t, VisibilityPrivate, p.Visibility, "bawaannya privat")
	assert.Equal(t, int64(8), p.OwnerID)

	pub, err := svc.CreatePlaylist(ctx, CreatePlaylistInput{RequesterID: 8, Title: "Musik", Visibility: "public"})
	require.NoError(t, err)
	assert.Equal(t, VisibilityPublic, pub.Visibility, "huruf kecil diterima")

	for name, in := range map[string]CreatePlaylistInput{
		"judul kosong":              {RequesterID: 8, Title: "   "},
		"visibilitas salah":         {RequesterID: 8, Title: "A", Visibility: "UNLISTED"},
		"deskripsi terlalu panjang": {RequesterID: 8, Title: "A", Description: string(make([]rune, maxDescriptionRunes+1))},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := svc.CreatePlaylist(ctx, in)
			var verr *ValidationError
			assert.ErrorAs(t, err, &verr)
		})
	}
	t.Run("tanpa requester", func(t *testing.T) {
		_, err := svc.CreatePlaylist(ctx, CreatePlaylistInput{Title: "A"})
		assert.Error(t, err)
	})
}

func TestCreatePlaylistLimit(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	for i := range maxPlaylistsPerUser {
		repo.playlists[int64(1000+i)] = Playlist{ID: int64(1000 + i), OwnerID: 8}
	}
	_, err := svc.CreatePlaylist(ctx, CreatePlaylistInput{RequesterID: 8, Title: "Satu lagi"})
	var verr *ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "playlist", verr.Field)

	_, err = svc.CreatePlaylist(ctx, CreatePlaylistInput{RequesterID: 9, Title: "User lain"})
	assert.NoError(t, err, "batasnya per akun")
}

func TestUpdateAndDeletePlaylist(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	svc.now = clockAt(time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC))
	p, err := svc.CreatePlaylist(ctx, CreatePlaylistInput{RequesterID: 8, Title: "Lama", Description: "isi"})
	require.NoError(t, err)

	title, public := "Baru", "PUBLIC"
	got, err := svc.UpdatePlaylist(ctx, UpdatePlaylistInput{RequesterID: 8, PlaylistID: p.ID, Title: &title, Visibility: &public})
	require.NoError(t, err)
	assert.Equal(t, "Baru", got.Title)
	assert.Equal(t, "isi", got.Description, "tidak dikirim = tidak diubah")
	assert.Equal(t, VisibilityPublic, got.Visibility)
	assert.True(t, got.UpdatedAt.After(p.UpdatedAt))

	empty := ""
	got, err = svc.UpdatePlaylist(ctx, UpdatePlaylistInput{RequesterID: 8, PlaylistID: p.ID, Description: &empty})
	require.NoError(t, err)
	assert.Empty(t, got.Description, "deskripsi boleh dihapus")

	t.Run("tanpa perubahan", func(t *testing.T) {
		_, err := svc.UpdatePlaylist(ctx, UpdatePlaylistInput{RequesterID: 8, PlaylistID: p.ID})
		var verr *ValidationError
		assert.ErrorAs(t, err, &verr)
	})
	t.Run("milik orang lain", func(t *testing.T) {
		_, err := svc.UpdatePlaylist(ctx, UpdatePlaylistInput{RequesterID: 9, PlaylistID: p.ID, Title: &title})
		assert.ErrorIs(t, err, ErrPlaylistNotFound, "walaupun PUBLIC")
		assert.ErrorIs(t, svc.DeletePlaylist(ctx, 9, p.ID), ErrPlaylistNotFound)
	})

	v := readyVideo(repo, 1, time.Now())
	_, err = svc.AddToPlaylist(ctx, 8, p.ID, v.ID)
	require.NoError(t, err)
	require.NoError(t, svc.DeletePlaylist(ctx, 8, p.ID))
	_, err = svc.GetPlaylist(ctx, 8, p.ID)
	assert.ErrorIs(t, err, ErrPlaylistNotFound)
	assert.Empty(t, repo.items[p.ID], "isinya ikut terhapus")
	_, ok := repo.videos[v.ID]
	assert.True(t, ok, "videonya tidak terhapus")
}

func TestPlaylistVisibility(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	priv, err := svc.CreatePlaylist(ctx, CreatePlaylistInput{RequesterID: 8, Title: "Rahasia"})
	require.NoError(t, err)
	pub, err := svc.CreatePlaylist(ctx, CreatePlaylistInput{RequesterID: 8, Title: "Umum", Visibility: VisibilityPublic})
	require.NoError(t, err)
	v := readyVideo(repo, 1, time.Now())
	for _, id := range []int64{priv.ID, pub.ID} {
		_, err := svc.AddToPlaylist(ctx, 8, id, v.ID)
		require.NoError(t, err)
	}

	// Pemilik melihat keduanya, orang lain dan tamu hanya yang PUBLIC.
	for name, tc := range map[string]struct {
		viewer int64
		want   []int64
	}{
		"pemilik":     {8, []int64{pub.ID, priv.ID}},
		"user lain":   {9, []int64{pub.ID}},
		"tanpa login": {0, []int64{pub.ID}},
	} {
		t.Run(name, func(t *testing.T) {
			list, _, err := svc.ListPlaylists(ctx, ListPlaylistsInput{ViewerID: tc.viewer, OwnerID: 8})
			require.NoError(t, err)
			assert.Equal(t, tc.want, playlistIDs(list))

			_, err = svc.GetPlaylist(ctx, tc.viewer, priv.ID)
			_, _, listErr := svc.ListPlaylistVideos(ctx, tc.viewer, priv.ID, 0, "")
			if tc.viewer == 8 {
				assert.NoError(t, err)
				assert.NoError(t, listErr)
			} else {
				assert.ErrorIs(t, err, ErrPlaylistNotFound)
				assert.ErrorIs(t, listErr, ErrPlaylistNotFound)
			}
			got, err := svc.GetPlaylist(ctx, tc.viewer, pub.ID)
			require.NoError(t, err)
			assert.Equal(t, int64(1), got.VideoCount)
		})
	}

	t.Run("video_id hanya untuk playlist sendiri", func(t *testing.T) {
		_, _, err := svc.ListPlaylists(ctx, ListPlaylistsInput{ViewerID: 9, OwnerID: 8, VideoID: v.ID})
		var verr *ValidationError
		assert.ErrorAs(t, err, &verr)
	})
	t.Run("playlist tidak ada", func(t *testing.T) {
		_, err := svc.GetPlaylist(ctx, 8, 999)
		assert.ErrorIs(t, err, ErrPlaylistNotFound)
		_, err = svc.GetPlaylist(ctx, 8, 0)
		assert.ErrorIs(t, err, ErrPlaylistNotFound)
	})
}

func TestAddToPlaylist(t *testing.T) {
	ctx := context.Background()
	svc, repo, store := newTestService()
	start := time.Date(2026, 11, 11, 8, 0, 0, 123456789, time.UTC)
	svc.now = clockAt(start)
	p, err := svc.CreatePlaylist(ctx, CreatePlaylistInput{RequesterID: 8, Title: "Daftar"})
	require.NoError(t, err)
	v := readyVideo(repo, 1, time.Now())

	at, err := svc.AddToPlaylist(ctx, 8, p.ID, v.ID)
	require.NoError(t, err)
	assert.Equal(t, start.Add(time.Second).Truncate(time.Microsecond), at, "dibulatkan ke mikrodetik")
	again, err := svc.AddToPlaylist(ctx, 8, p.ID, v.ID)
	require.NoError(t, err)
	assert.Equal(t, at, again, "posisinya tidak berubah")
	assert.Len(t, repo.items[p.ID], 1)

	// GetVideo memberi tahu tombol Simpan, hanya untuk pemilik playlist.
	d, err := svc.GetVideo(ctx, 8, v.ID)
	require.NoError(t, err)
	assert.True(t, d.ViewerInPlaylist)
	d, err = svc.GetVideo(ctx, 9, v.ID)
	require.NoError(t, err)
	assert.False(t, d.ViewerInPlaylist)

	// ContainsVideo untuk dialog Simpan.
	other, err := svc.CreatePlaylist(ctx, CreatePlaylistInput{RequesterID: 8, Title: "Lain"})
	require.NoError(t, err)
	list, _, err := svc.ListPlaylists(ctx, ListPlaylistsInput{ViewerID: 8, OwnerID: 8, VideoID: v.ID})
	require.NoError(t, err)
	require.Equal(t, []int64{other.ID, p.ID}, playlistIDs(list))
	assert.False(t, list[0].ContainsVideo)
	assert.True(t, list[1].ContainsVideo)

	require.NoError(t, svc.RemoveFromPlaylist(ctx, 8, p.ID, v.ID))
	require.NoError(t, svc.RemoveFromPlaylist(ctx, 8, p.ID, v.ID), "aman diulang")
	d, err = svc.GetVideo(ctx, 8, v.ID)
	require.NoError(t, err)
	assert.False(t, d.ViewerInPlaylist)

	t.Run("playlist orang lain", func(t *testing.T) {
		_, err := svc.AddToPlaylist(ctx, 9, p.ID, v.ID)
		assert.ErrorIs(t, err, ErrPlaylistNotFound)
		assert.ErrorIs(t, svc.RemoveFromPlaylist(ctx, 9, p.ID, v.ID), ErrPlaylistNotFound)
	})
	t.Run("video belum READY atau tidak ada", func(t *testing.T) {
		up := uploadedVideo(t, svc, store)
		_, err := svc.AddToPlaylist(ctx, 8, p.ID, up.ID)
		assert.ErrorIs(t, err, ErrNotFound)
		_, err = svc.AddToPlaylist(ctx, 8, p.ID, 999)
		assert.ErrorIs(t, err, ErrNotFound)
	})
	t.Run("playlist penuh", func(t *testing.T) {
		full, err := svc.CreatePlaylist(ctx, CreatePlaylistInput{RequesterID: 8, Title: "Penuh"})
		require.NoError(t, err)
		repo.items[full.ID] = map[int64]time.Time{}
		for i := range maxPlaylistVideos {
			repo.items[full.ID][int64(5000+i)] = start
		}
		_, err = svc.AddToPlaylist(ctx, 8, full.ID, v.ID)
		var verr *ValidationError
		assert.ErrorAs(t, err, &verr)

		repo.items[full.ID][v.ID] = start
		_, err = svc.AddToPlaylist(ctx, 8, full.ID, v.ID)
		assert.NoError(t, err, "video yang sudah ada tetap boleh ditambah ulang")
	})
}

func TestListPlaylistVideos(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	svc.now = clockAt(time.Date(2026, 11, 11, 8, 0, 0, 0, time.UTC))
	p, err := svc.CreatePlaylist(ctx, CreatePlaylistInput{RequesterID: 8, Title: "Urut", Visibility: VisibilityPublic})
	require.NoError(t, err)
	a := readyVideo(repo, 1, time.Now())
	b := readyVideo(repo, 1, time.Now())
	c := readyVideo(repo, 1, time.Now())
	repo.mu.Lock()
	vb := repo.videos[b.ID]
	vb.ThumbnailKey = "hls/b/thumb.jpg"
	repo.videos[b.ID] = vb
	repo.mu.Unlock()

	// Ditambahkan b, a, c: urutan playlist mengikuti waktu ditambahkan.
	for _, id := range []int64{b.ID, a.ID, c.ID, b.ID} {
		_, err := svc.AddToPlaylist(ctx, 8, p.ID, id)
		require.NoError(t, err)
	}

	page1, next, err := svc.ListPlaylistVideos(ctx, 0, p.ID, 2, "")
	require.NoError(t, err)
	assert.Equal(t, []int64{b.ID, a.ID}, entryIDs(page1))
	require.NotEmpty(t, next)
	page2, next, err := svc.ListPlaylistVideos(ctx, 0, p.ID, 2, next)
	require.NoError(t, err)
	assert.Equal(t, []int64{c.ID}, entryIDs(page2))
	assert.Empty(t, next)

	got, err := svc.GetPlaylist(ctx, 0, p.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(3), got.VideoCount)
	assert.Equal(t, "hls/b/thumb.jpg", got.ThumbnailKey, "thumbnail video pertama")

	t.Run("video yang tidak READY lagi tidak dihitung", func(t *testing.T) {
		repo.mu.Lock()
		vb := repo.videos[b.ID]
		vb.Status = StatusFailed
		repo.videos[b.ID] = vb
		repo.mu.Unlock()
		all, _, err := svc.ListPlaylistVideos(ctx, 0, p.ID, 0, "")
		require.NoError(t, err)
		assert.Equal(t, []int64{a.ID, c.ID}, entryIDs(all))
		got, err := svc.GetPlaylist(ctx, 0, p.ID)
		require.NoError(t, err)
		assert.Equal(t, int64(2), got.VideoCount)
	})
}

func TestPlaylistPageValidation(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService()
	p, err := svc.CreatePlaylist(ctx, CreatePlaylistInput{RequesterID: 8, Title: "A"})
	require.NoError(t, err)

	var verr *ValidationError
	_, _, err = svc.ListPlaylists(ctx, ListPlaylistsInput{ViewerID: 8, OwnerID: 8, PageSize: maxPageSize + 1})
	require.ErrorAs(t, err, &verr)
	_, _, err = svc.ListPlaylistVideos(ctx, 8, p.ID, maxPageSize+1, "")
	require.ErrorAs(t, err, &verr)

	entryToken := encodePlaylistEntryCursor(PlaylistEntryCursor{AddedAt: time.Now(), VideoID: 1})
	for name, token := range map[string]string{
		"bukan base64":   "%%%",
		"token isi":      entryToken,
		"id nol":         base64.RawURLEncoding.EncodeToString([]byte("p0")),
		"id bukan angka": base64.RawURLEncoding.EncodeToString([]byte("pabc")),
	} {
		t.Run("daftar/"+name, func(t *testing.T) {
			_, _, err := svc.ListPlaylists(ctx, ListPlaylistsInput{ViewerID: 8, OwnerID: 8, PageToken: token})
			var verr *ValidationError
			require.ErrorAs(t, err, &verr)
			assert.Equal(t, "page_token", verr.Field)
		})
	}
	for name, token := range map[string]string{
		"token daftar": encodePlaylistCursor(5),
		"tanpa titik":  base64.RawURLEncoding.EncodeToString([]byte("i123")),
		"waktu nol":    base64.RawURLEncoding.EncodeToString([]byte("i0.5")),
	} {
		t.Run("isi/"+name, func(t *testing.T) {
			_, _, err := svc.ListPlaylistVideos(ctx, 8, p.ID, 0, token)
			var verr *ValidationError
			require.ErrorAs(t, err, &verr)
		})
	}
}

func TestPlaylistCursorRoundTrip(t *testing.T) {
	id, err := decodePlaylistCursor(encodePlaylistCursor(42))
	require.NoError(t, err)
	assert.Equal(t, int64(42), id)

	c := PlaylistEntryCursor{AddedAt: time.Date(2026, 11, 11, 8, 0, 0, 123456000, time.UTC), VideoID: 7}
	got, err := decodePlaylistEntryCursor(encodePlaylistEntryCursor(c))
	require.NoError(t, err)
	assert.Equal(t, c, got)
}
