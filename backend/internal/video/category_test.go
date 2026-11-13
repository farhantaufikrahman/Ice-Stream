package video

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanCategory(t *testing.T) {
	for in, want := range map[string]string{"": "", "  Game ": "game", "musik": "musik", "OLAHRAGA": "olahraga"} {
		got, err := cleanCategory(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	var verr *ValidationError
	for _, in := range []string{"film", "game,musik", "ga me"} {
		_, err := cleanCategory(in)
		require.ErrorAs(t, err, &verr, in)
		assert.Equal(t, "category", verr.Field)
	}
}

func TestCleanTags(t *testing.T) {
	got, err := cleanTags([]string{" #esKrim", "anakKos", "", "ESKRIM", "#", "Café", "café", "2026"})
	require.NoError(t, err)
	// "#" dibuang, kosong dilewati, dobel (huruf besar/kecil, atau é yang
	// diketik dengan tanda aksen terpisah) hanya disimpan sekali.
	assert.Equal(t, []string{"esKrim", "anakKos", "Café", "2026"}, got)

	got, err = cleanTags(nil)
	require.NoError(t, err)
	assert.Empty(t, got)

	ten := make([]string, 10)
	for i := range ten {
		ten[i] = "tag" + string(rune('a'+i))
	}
	_, err = cleanTags(ten)
	require.NoError(t, err, "10 tag masih boleh")

	var verr *ValidationError
	for name, in := range map[string][]string{
		"11 tag":            append(ten, "tagk"),
		"spasi":             {"es krim"},
		"tanda baca":        {"es-krim"},
		"garis bawah":       {"es_krim"},
		"satu huruf":        {"a"},
		"terlalu panjang":   {strings.Repeat("a", 31)},
		"bukan UTF-8":       {"ab\xff"},
		"emoji bukan huruf": {"es🍦"},
	} {
		_, err := cleanTags(in)
		require.ErrorAs(t, err, &verr, name)
		assert.Equal(t, "tags", verr.Field, name)
	}
}

func TestSplitJoinTags(t *testing.T) {
	assert.Nil(t, splitTags(""))
	assert.Equal(t, []string{"esKrim", "anakKos"}, splitTags(joinTags([]string{"esKrim", "anakKos"})))
}

func TestBuildSearchTermsWithTags(t *testing.T) {
	terms := BuildSearchTerms("Gelato", "resep es krim", []string{"esKrim", "Gelato"})
	assert.Equal(t, []SearchTerm{
		{"gelato", WeightTitle},
		{"eskrim", WeightTitle},
		{"resep", WeightDescription},
		{"es", WeightDescription},
		{"krim", WeightDescription},
	}, terms)
}

func TestCreateUploadCategoryTags(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	in := validInput()
	in.Category = "Kuliner"
	in.Tags = []string{"#esKrim", "anakKos", "eskrim"}

	up, err := svc.CreateUpload(ctx, in)
	require.NoError(t, err)
	assert.Equal(t, "kuliner", up.Video.Category)
	assert.Equal(t, []string{"esKrim", "anakKos"}, up.Video.Tags)
	assert.Contains(t, repo.terms[up.Video.ID], SearchTerm{"eskrim", WeightTitle}, "tag ikut dicari")

	var verr *ValidationError
	in.Category = "film"
	_, err = svc.CreateUpload(ctx, in)
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "category", verr.Field)

	in.Category = ""
	in.Tags = []string{"es krim"}
	_, err = svc.CreateUpload(ctx, in)
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "tags", verr.Field)
}

func TestUpdateVideoCategoryTags(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	in := validInput()
	in.Category, in.Tags = "game", []string{"horor"}
	up, err := svc.CreateUpload(ctx, in)
	require.NoError(t, err)
	id := up.Video.ID

	// Hanya judul: kategori dan tag tetap.
	v, err := svc.UpdateVideo(ctx, UpdateInput{RequesterID: 7, VideoID: id, Title: ptr("Main game")})
	require.NoError(t, err)
	assert.Equal(t, "game", v.Category)
	assert.Equal(t, []string{"horor"}, v.Tags)
	assert.Contains(t, repo.terms[id], SearchTerm{"horor", WeightTitle}, "tag lama tetap dicari")

	// Ganti kategori dan tag saja.
	v, err = svc.UpdateVideo(ctx, UpdateInput{RequesterID: 7, VideoID: id, Category: ptr("Musik"), Tags: []string{"lofi"}, SetTags: true})
	require.NoError(t, err)
	assert.Equal(t, "musik", v.Category)
	assert.Equal(t, []string{"lofi"}, v.Tags)
	assert.Equal(t, "Main game", v.Title)
	assert.NotContains(t, repo.terms[id], SearchTerm{"horor", WeightTitle}, "tag lama tidak dicari lagi")

	// "" menghapus kategori, SetTags tanpa isi menghapus semua tag.
	v, err = svc.UpdateVideo(ctx, UpdateInput{RequesterID: 7, VideoID: id, Category: ptr(""), SetTags: true})
	require.NoError(t, err)
	assert.Empty(t, v.Category)
	assert.Empty(t, v.Tags)

	var verr *ValidationError
	_, err = svc.UpdateVideo(ctx, UpdateInput{RequesterID: 7, VideoID: id, Category: ptr("film")})
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "category", verr.Field)
	_, err = svc.UpdateVideo(ctx, UpdateInput{RequesterID: 7, VideoID: id, Tags: []string{"a"}, SetTags: true})
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "tags", verr.Field)
}

func TestListVideosByCategory(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	now := time.Now()
	game1 := readyVideo(repo, 7, now.Add(-2*time.Hour))
	music := readyVideo(repo, 7, now.Add(-time.Hour))
	game2 := readyVideo(repo, 8, now)
	readyVideo(repo, 8, now) // tanpa kategori
	for id, c := range map[int64]string{game1.ID: "game", music.ID: "musik", game2.ID: "game"} {
		v := repo.videos[id]
		v.Category = c
		repo.videos[id] = v
	}

	page, next, err := svc.ListVideos(ctx, FeedFilter{Category: "Game"}, 1, "")
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, game2.ID, page[0].ID, "terbaru lebih dulu")
	require.NotEmpty(t, next)
	page, next, err = svc.ListVideos(ctx, FeedFilter{Category: "game"}, 1, next)
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, game1.ID, page[0].ID)
	assert.Empty(t, next)

	// Kategori bisa digabung dengan pemilik.
	page, _, err = svc.ListVideos(ctx, FeedFilter{Category: "game", OwnerIDs: []int64{7}}, 0, "")
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, game1.ID, page[0].ID)

	all, _, err := svc.ListVideos(ctx, FeedFilter{}, 0, "")
	require.NoError(t, err)
	assert.Len(t, all, 4, "tanpa filter, video tanpa kategori ikut tampil")

	var verr *ValidationError
	_, _, err = svc.ListVideos(ctx, FeedFilter{Category: "film"}, 0, "")
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "category", verr.Field)
}
