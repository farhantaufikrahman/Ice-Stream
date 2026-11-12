package video

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// transcoded membuat video milik user 7 sampai READY, dengan pilihan dari
// in (visibilitas, draf, bahasa).
func transcoded(t *testing.T, svc *Service, store *fakeStore, in CreateUploadInput) Video {
	t.Helper()
	ctx := context.Background()
	up, err := svc.CreateUpload(ctx, in)
	require.NoError(t, err)
	store.objects[up.Video.ObjectKey] = ObjectInfo{Size: in.SizeBytes, ContentType: "video/mp4"}
	_, err = svc.CompleteUpload(ctx, in.OwnerID, up.Video.ID)
	require.NoError(t, err)
	require.NoError(t, svc.MarkReady(ctx, up.Video.ID, TranscodeResult{
		PlaylistKey:  "hls/1/abc/master.m3u8",
		ThumbnailKey: "hls/1/abc/frames/3000.jpg",
		DurationMs:   10000,
		Renditions:   []string{"360p"},
		FrameTimesMs: []int64{3000, 4000, 7500},
	}))
	v, err := svc.repo.FindByID(ctx, up.Video.ID)
	require.NoError(t, err)
	return v
}

func TestVisibilityRules(t *testing.T) {
	ctx := context.Background()
	svc, _, store, pub := newTestServiceWithEvents()

	in := validInput()
	public := transcoded(t, svc, store, in)
	in.Visibility = "unlisted"
	unlisted := transcoded(t, svc, store, in)
	in.Visibility = "PRIVATE"
	private := transcoded(t, svc, store, in)
	in.Visibility, in.Draft = "", true
	draft := transcoded(t, svc, store, in)

	assert.Equal(t, VisibilityUnlisted, unlisted.Visibility, "huruf kecil diterima")
	assert.Equal(t, VisibilityPublic, draft.Visibility, "kosong = PUBLIC")
	assert.NotNil(t, public.PublishedAt)
	assert.Nil(t, draft.PublishedAt, "draf belum terbit")
	// Hanya video publik yang memicu notifikasi subscriber.
	assert.Equal(t, []int64{public.ID}, pub.ready)

	// Feed hanya berisi video publik.
	feed, _, err := svc.ListVideos(ctx, FeedFilter{}, 0, "")
	require.NoError(t, err)
	assert.Equal(t, []int64{public.ID}, ids(feed))

	// Orang lain dan pengunjung: publik dan Tidak publik bisa dibuka lewat
	// tautan; Pribadi dan draf seolah tidak ada.
	for _, viewer := range []int64{0, 99} {
		for _, v := range []Video{public, unlisted} {
			_, err := svc.GetVideo(ctx, viewer, v.ID)
			assert.NoError(t, err, "viewer %d video %s", viewer, v.Visibility)
		}
		for _, v := range []Video{private, draft} {
			_, err := svc.GetVideo(ctx, viewer, v.ID)
			assert.ErrorIs(t, err, ErrNotFound, "viewer %d video %d", viewer, v.ID)
		}
	}
	// Pemilik bisa membuka semuanya.
	for _, v := range []Video{public, unlisted, private, draft} {
		_, err := svc.GetVideo(ctx, 7, v.ID)
		assert.NoError(t, err)
	}

	// Views dan like hanya untuk video yang boleh ditonton orang lain.
	_, err = svc.RecordView(ctx, unlisted.ID, "a-1")
	assert.NoError(t, err)
	_, err = svc.RecordView(ctx, private.ID, "a-1")
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = svc.LikeVideo(ctx, 99, draft.ID)
	assert.ErrorIs(t, err, ErrNotFound)

	// Pemilik boleh menyimpan riwayat dan Tonton nanti untuk video
	// Pribadinya sendiri; orang lain tidak.
	_, err = svc.SaveWatchProgress(ctx, 7, private.ID, 1000)
	assert.NoError(t, err)
	_, err = svc.SaveWatchProgress(ctx, 99, private.ID, 1000)
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = svc.AddToWatchLater(ctx, 99, draft.ID)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestPublishDraft(t *testing.T) {
	ctx := context.Background()
	svc, _, store, pub := newTestServiceWithEvents()
	in := validInput()
	in.Draft = true
	in.Language = "JA"
	draft := transcoded(t, svc, store, in)
	assert.Equal(t, "ja", draft.Language)
	assert.Empty(t, pub.ready)

	// Terbitkan sebagai Tidak publik: tetap tidak ada notifikasi, tapi
	// waktu terbit sudah terisi.
	no, unlisted := false, VisibilityUnlisted
	v, err := svc.UpdateVideo(ctx, UpdateInput{RequesterID: 7, VideoID: draft.ID, Draft: &no, Visibility: &unlisted})
	require.NoError(t, err)
	assert.False(t, v.Draft)
	require.NotNil(t, v.PublishedAt)
	firstPublished := *v.PublishedAt
	assert.Empty(t, pub.ready)

	// Ubah menjadi Publik: sekarang subscriber diberi tahu, dan waktu terbit
	// tidak berubah.
	public := "public"
	v, err = svc.UpdateVideo(ctx, UpdateInput{RequesterID: 7, VideoID: draft.ID, Visibility: &public})
	require.NoError(t, err)
	assert.True(t, v.IsListed())
	assert.Equal(t, []int64{draft.ID}, pub.ready)
	assert.True(t, firstPublished.Equal(*v.PublishedAt))

	// Simpan lagi tanpa perubahan visibilitas: tidak ada event lagi.
	title := "Judul baru"
	_, err = svc.UpdateVideo(ctx, UpdateInput{RequesterID: 7, VideoID: draft.ID, Title: &title})
	require.NoError(t, err)
	assert.Len(t, pub.ready, 1)

	var verr *ValidationError
	bad := "SECRET"
	_, err = svc.UpdateVideo(ctx, UpdateInput{RequesterID: 7, VideoID: draft.ID, Visibility: &bad})
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "visibility", verr.Field)
	lang := "fr"
	_, err = svc.UpdateVideo(ctx, UpdateInput{RequesterID: 7, VideoID: draft.ID, Language: &lang})
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "language", verr.Field)

	_, err = svc.CreateUpload(ctx, CreateUploadInput{OwnerID: 7, Title: "x", ContentType: "video/mp4", SizeBytes: 1, Visibility: "everyone"})
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "visibility", verr.Field)
}

func testImage(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		img.Set(x, h/2, color.RGBA{R: 255, A: 255})
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func TestProcessThumbnail(t *testing.T) {
	out, err := ProcessThumbnail(testImage(t, 1000, 1000))
	require.NoError(t, err)
	img, err := jpeg.Decode(bytes.NewReader(out))
	require.NoError(t, err)
	assert.Equal(t, image.Rect(0, 0, 1280, 720), img.Bounds(), "selalu 16:9")

	var verr *ValidationError
	_, err = ProcessThumbnail([]byte("bukan gambar"))
	assert.ErrorAs(t, err, &verr)
	_, err = ProcessThumbnail(testImage(t, 200, 100))
	assert.ErrorAs(t, err, &verr, "terlalu kecil")
	_, err = ProcessThumbnail(nil)
	assert.ErrorAs(t, err, &verr)

	assert.Equal(t, image.Rect(0, 219, 1000, 781), center16x9(image.Rect(0, 0, 1000, 1000)))
	assert.Equal(t, image.Rect(100, 0, 1700, 900), center16x9(image.Rect(0, 0, 1800, 900)))
}

func TestChooseAndUploadThumbnail(t *testing.T) {
	ctx := context.Background()
	svc, _, store, _ := newTestServiceWithEvents()
	v := transcoded(t, svc, store, validInput())
	assert.Equal(t, []int64{3000, 4000, 7500}, v.ThumbnailFrames)
	assert.Equal(t, "hls/1/abc/frames/7500.jpg", FrameKey(v, 7500))

	got, err := svc.ChooseThumbnailFrame(ctx, 7, v.ID, 7500)
	require.NoError(t, err)
	assert.Equal(t, "hls/1/abc/frames/7500.jpg", got.ThumbnailKey)

	_, err = svc.ChooseThumbnailFrame(ctx, 7, v.ID, 1234)
	assert.ErrorIs(t, err, ErrFrameNotFound, "bukan posisi dari transcoder")
	_, err = svc.ChooseThumbnailFrame(ctx, 99, v.ID, 3000)
	assert.ErrorIs(t, err, ErrNotFound, "bukan pemilik")

	// Unggah gambar sendiri: tersimpan sebagai JPEG dengan nama acak.
	got, err = svc.UploadThumbnail(ctx, 7, v.ID, testImage(t, 640, 360))
	require.NoError(t, err)
	first := got.ThumbnailKey
	assert.True(t, strings.HasPrefix(first, "hls/1/custom-"), first)
	assert.Contains(t, store.objects, first)

	// Gambar baru menggantikan yang lama, dan file lama dihapus.
	got, err = svc.UploadThumbnail(ctx, 7, v.ID, testImage(t, 640, 360))
	require.NoError(t, err)
	assert.NotEqual(t, first, got.ThumbnailKey)
	assert.NotContains(t, store.objects, first)

	// Kembali ke bingkai: gambar buatan kreator ikut dihapus.
	custom := got.ThumbnailKey
	_, err = svc.ChooseThumbnailFrame(ctx, 7, v.ID, 3000)
	require.NoError(t, err)
	assert.NotContains(t, store.objects, custom)

	var verr *ValidationError
	_, err = svc.UploadThumbnail(ctx, 7, v.ID, []byte("bukan gambar"))
	assert.ErrorAs(t, err, &verr)
}

func TestCustomThumbnailSurvivesTranscoding(t *testing.T) {
	ctx := context.Background()
	svc, _, store, _ := newTestServiceWithEvents()
	up, err := svc.CreateUpload(ctx, validInput())
	require.NoError(t, err)
	// Gambar diunggah saat video masih diunggah/diproses.
	withThumb, err := svc.UploadThumbnail(ctx, 7, up.Video.ID, testImage(t, 640, 360))
	require.NoError(t, err)

	store.objects[up.Video.ObjectKey] = ObjectInfo{Size: 1234, ContentType: "video/mp4"}
	_, err = svc.CompleteUpload(ctx, 7, up.Video.ID)
	require.NoError(t, err)
	require.NoError(t, svc.MarkReady(ctx, up.Video.ID, TranscodeResult{
		PlaylistKey: "hls/1/abc/master.m3u8", ThumbnailKey: "hls/1/abc/frames/3000.jpg",
		DurationMs: 10000, Renditions: []string{"360p"}, FrameTimesMs: []int64{3000},
	}))
	v, err := svc.repo.FindByID(ctx, up.Video.ID)
	require.NoError(t, err)
	assert.Equal(t, withThumb.ThumbnailKey, v.ThumbnailKey, "thumbnail kreator tidak ditimpa transcoder")
}
