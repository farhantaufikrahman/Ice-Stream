package video

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SearchPlaylists palsu meniru urutan query Oracle: judul yang diawali kata
// pertama dulu, lalu updated_at terbaru, lalu id terbesar.
func (r *fakeRepo) SearchPlaylists(_ context.Context, terms []string, offset, limit int) ([]Playlist, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Playlist
	for _, p := range r.playlists {
		title := strings.ToLower(p.Title)
		ok := p.Visibility == VisibilityPublic && len(r.readyItemsLocked(p.ID)) > 0
		for _, t := range terms {
			ok = ok && strings.Contains(title, t)
		}
		if ok {
			out = append(out, p)
		}
	}
	rank := func(p Playlist) int {
		if strings.HasPrefix(strings.ToLower(p.Title), terms[0]) {
			return 0
		}
		return 1
	}
	slices.SortFunc(out, func(a, b Playlist) int {
		if d := rank(a) - rank(b); d != 0 {
			return d
		}
		if c := b.UpdatedAt.Compare(a.UpdatedAt); c != 0 {
			return c
		}
		return int(b.ID - a.ID)
	})
	if offset >= len(out) {
		return nil, nil
	}
	out = out[offset:]
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func TestSearchPlaylists(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	svc.now = clockAt(time.Date(2026, 11, 11, 8, 0, 0, 0, time.UTC))
	v := readyVideo(repo, 7, time.Now())

	mk := func(owner int64, title, visibility string, withVideo bool) Playlist {
		p, err := svc.CreatePlaylist(ctx, CreatePlaylistInput{RequesterID: owner, Title: title, Visibility: visibility})
		require.NoError(t, err)
		if withVideo {
			_, err = svc.AddToPlaylist(ctx, owner, p.ID, v.ID)
			require.NoError(t, err)
		}
		return p
	}
	older := mk(8, "Kumpulan belajar Go", "PUBLIC", true)
	prefix := mk(9, "Belajar Go dari nol", "PUBLIC", true)
	newer := mk(8, "Seri belajar golang lanjut", "PUBLIC", true)
	mk(8, "Belajar Go privat", "PRIVATE", true) // privat: tidak ikut
	mk(8, "Belajar Go kosong", "PUBLIC", false) // tanpa video: tidak ikut
	mk(8, "Musik santai", "PUBLIC", true)       // tidak cocok

	list, next, err := svc.SearchPlaylists(ctx, "BELAJAR go", 0, "")
	require.NoError(t, err)
	ids := make([]int64, len(list))
	for i, p := range list {
		ids[i] = p.ID
	}
	// Diawali "belajar" dulu, lalu yang terakhir diperbarui.
	assert.Equal(t, []int64{prefix.ID, newer.ID, older.ID}, ids)
	assert.Empty(t, next)
	assert.Equal(t, int64(1), list[0].VideoCount, "statistik ikut diisi")
	assert.Equal(t, v.ThumbnailKey, list[0].ThumbnailKey)

	// Halaman kedua.
	list, next, err = svc.SearchPlaylists(ctx, "belajar", 2, "")
	require.NoError(t, err)
	assert.Len(t, list, 2)
	require.NotEmpty(t, next)
	list, _, err = svc.SearchPlaylists(ctx, "belajar", 2, next)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, older.ID, list[0].ID)

	var verr *ValidationError
	for name, call := range map[string]func() error{
		"query kosong":    func() error { _, _, err := svc.SearchPlaylists(ctx, "!", 0, ""); return err },
		"page_size besar": func() error { _, _, err := svc.SearchPlaylists(ctx, "go", 51, ""); return err },
		"token rusak":     func() error { _, _, err := svc.SearchPlaylists(ctx, "go", 0, "xyz"); return err },
	} {
		assert.ErrorAs(t, call(), &verr, name)
	}
}
