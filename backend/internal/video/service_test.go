package video

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRepo menyimpan video di memori, menggantikan Oracle saat test.
type fakeRepo struct {
	mu     sync.Mutex
	nextID int64
	videos map[int64]Video
	// likes[videoID][userID] = true jika user itu sudah like.
	likes map[int64]map[int64]bool
	// incrementErr diisi test untuk mensimulasikan Oracle yang gagal.
	incrementErr error
	// terms[videoID] = kata pencarian; video yang tidak ada di map ini
	// belum diindeks.
	terms map[int64][]SearchTerm
	// history[userID][videoID] = entri riwayat (Video hanya berisi ID).
	history map[int64]map[int64]HistoryEntry
	// trash berisi video yang sudah dihapus (soft delete). purged[id] = true
	// jika file-nya sudah dihapus dari storage.
	trash  map[int64]Video
	purged map[int64]bool
	// notifs berisi semua notifikasi, urut id (lihat notification_test.go).
	notifs []fakeNotif
	// later[userID][videoID] = waktu disimpan ke Tonton nanti.
	later map[int64]map[int64]time.Time
	// playlists[id] dan items[playlistID][videoID] = waktu ditambahkan
	// (lihat playlist_test.go).
	playlists map[int64]Playlist
	items     map[int64]map[int64]time.Time
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{videos: map[int64]Video{}, likes: map[int64]map[int64]bool{}, terms: map[int64][]SearchTerm{}, history: map[int64]map[int64]HistoryEntry{},
		trash: map[int64]Video{}, purged: map[int64]bool{}, later: map[int64]map[int64]time.Time{},
		playlists: map[int64]Playlist{}, items: map[int64]map[int64]time.Time{}}
}

func (r *fakeRepo) changeLike(videoID, userID int64, like bool) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.videos[videoID]
	if !ok || v.Status != StatusReady {
		return 0, ErrNotFound
	}
	if r.likes[videoID] == nil {
		r.likes[videoID] = map[int64]bool{}
	}
	if r.likes[videoID][userID] != like {
		r.likes[videoID][userID] = like
		if like {
			v.LikeCount++
		} else {
			v.LikeCount--
		}
		r.videos[videoID] = v
	}
	return v.LikeCount, nil
}

func (r *fakeRepo) Like(_ context.Context, videoID, userID int64) (int64, error) {
	return r.changeLike(videoID, userID, true)
}

func (r *fakeRepo) Unlike(_ context.Context, videoID, userID int64) (int64, error) {
	return r.changeLike(videoID, userID, false)
}

func (r *fakeRepo) IsLiked(_ context.Context, videoID, userID int64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.likes[videoID][userID], nil
}

func (r *fakeRepo) Create(_ context.Context, nv NewVideo) (Video, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	v := Video{
		ID: r.nextID, OwnerID: nv.OwnerID, Title: nv.Title, Description: nv.Description,
		Status: StatusPendingUpload, ObjectKey: nv.ObjectKey, ContentType: nv.ContentType,
		SizeBytes: nv.SizeBytes, CreatedAt: time.Now().UTC(),
	}
	r.videos[v.ID] = v
	r.terms[v.ID] = nv.SearchTerms
	return v, nil
}

func (r *fakeRepo) FindByID(_ context.Context, id int64) (Video, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.videos[id]
	if !ok {
		return Video{}, ErrNotFound
	}
	return v, nil
}

func (r *fakeRepo) MarkUploaded(_ context.Context, id int64, now time.Time) (Video, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.videos[id]
	if !ok || v.Status != StatusPendingUpload {
		return Video{}, ErrNotUploading
	}
	v.Status, v.UploadedAt = StatusUploaded, &now
	r.videos[id] = v
	return v, nil
}

func (r *fakeRepo) ListByOwner(_ context.Context, ownerID, beforeID int64, limit int) ([]Video, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Video
	for id := r.nextID; id > 0 && len(out) < limit; id-- {
		v, ok := r.videos[id]
		if ok && v.OwnerID == ownerID && (beforeID == 0 || id < beforeID) {
			out = append(out, v)
		}
	}
	return out, nil
}

func (r *fakeRepo) ListPublic(_ context.Context, ownerIDs []int64, after *FeedCursor, limit int) ([]Video, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var ready []Video
	for _, v := range r.videos {
		if v.Status == StatusReady && (len(ownerIDs) == 0 || slices.Contains(ownerIDs, v.OwnerID)) {
			ready = append(ready, v)
		}
	}
	// Sama dengan ORDER BY processed_at DESC, id DESC.
	slices.SortFunc(ready, func(a, b Video) int {
		if c := b.ProcessedAt.Compare(*a.ProcessedAt); c != 0 {
			return c
		}
		return int(b.ID - a.ID)
	})
	var out []Video
	for _, v := range ready {
		if after != nil && !v.ProcessedAt.Before(after.ProcessedAt) &&
			!(v.ProcessedAt.Equal(after.ProcessedAt) && v.ID < after.ID) {
			continue
		}
		if len(out) == limit {
			break
		}
		out = append(out, v)
	}
	return out, nil
}

func (r *fakeRepo) IncrementViews(_ context.Context, id int64) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.incrementErr != nil {
		return 0, r.incrementErr
	}
	v, ok := r.videos[id]
	if !ok || v.Status != StatusReady {
		return 0, ErrNotFound
	}
	v.ViewCount++
	r.videos[id] = v
	return v.ViewCount, nil
}

func (r *fakeRepo) transition(id int64, from []string, apply func(*Video)) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.videos[id]
	if !ok || !slices.Contains(from, v.Status) {
		return false, nil
	}
	apply(&v)
	r.videos[id] = v
	return true, nil
}

func (r *fakeRepo) MarkProcessing(_ context.Context, id int64, _ time.Time) (bool, error) {
	return r.transition(id, []string{StatusUploaded}, func(v *Video) { v.Status = StatusProcessing })
}

func (r *fakeRepo) MarkReady(_ context.Context, id int64, res TranscodeResult, now time.Time) (bool, error) {
	return r.transition(id, []string{StatusUploaded, StatusProcessing}, func(v *Video) {
		v.Status, v.PlaylistKey, v.ThumbnailKey = StatusReady, res.PlaylistKey, res.ThumbnailKey
		v.DurationMs, v.Renditions, v.ProcessedAt = res.DurationMs, res.Renditions, &now
	})
}

func (r *fakeRepo) MarkFailed(_ context.Context, id int64, reason string, now time.Time) (bool, error) {
	return r.transition(id, []string{StatusUploaded, StatusProcessing}, func(v *Video) {
		v.Status, v.FailureReason, v.ProcessedAt = StatusFailed, reason, &now
	})
}

// fakePublisher mencatat event yang dikirim. Isi err untuk mensimulasikan
// NATS yang sedang mati.
type fakePublisher struct {
	published []int64
	// ready berisi id video yang dikirim lewat PublishPublished.
	ready []int64
	err   error
}

func (p *fakePublisher) PublishPublished(_ context.Context, v Video) error {
	if p.err != nil {
		return p.err
	}
	p.ready = append(p.ready, v.ID)
	return nil
}

func (p *fakePublisher) PublishUploaded(_ context.Context, v Video) error {
	if p.err != nil {
		return p.err
	}
	p.published = append(p.published, v.ID)
	return nil
}

// fakeViews menggantikan NATS Key-Value: set berisi key yang sudah ditandai.
// Isi err untuk mensimulasikan NATS yang sedang mati.
type fakeViews struct {
	mu   sync.Mutex
	seen map[string]bool
	err  error
}

func (f *fakeViews) Claim(_ context.Context, videoID int64, viewerKey string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return false, f.err
	}
	k := viewKey(videoID, viewerKey)
	if f.seen[k] {
		return false, nil
	}
	f.seen[k] = true
	return true, nil
}

func (f *fakeViews) Release(_ context.Context, videoID int64, viewerKey string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.seen, viewKey(videoID, viewerKey))
	return nil
}

// fakeStore pura-pura menjadi object storage. objects diisi test untuk
// mensimulasikan file yang sudah diunggah browser.
type fakeStore struct {
	objects map[string]ObjectInfo
	// deleteErr diisi test untuk mensimulasikan storage yang mati saat
	// menghapus file.
	deleteErr error
}

func (s *fakeStore) PresignPut(_ context.Context, key, contentType string, _ int64, ttl time.Duration) (PresignedRequest, error) {
	return PresignedRequest{URL: "http://storage.test/videos/" + key + "?sig=put", Headers: map[string]string{"Content-Type": contentType}, ExpiresAt: time.Now().Add(ttl)}, nil
}

func (s *fakeStore) PresignGet(_ context.Context, key string, ttl time.Duration) (PresignedRequest, error) {
	return PresignedRequest{URL: "http://storage.test/videos/" + key + "?sig=get", ExpiresAt: time.Now().Add(ttl)}, nil
}

func (s *fakeStore) Stat(_ context.Context, key string) (ObjectInfo, error) {
	info, ok := s.objects[key]
	if !ok {
		return ObjectInfo{}, ErrObjectNotFound
	}
	return info, nil
}

func newTestService() (*Service, *fakeRepo, *fakeStore) {
	svc, repo, store, _ := newTestServiceWithEvents()
	return svc, repo, store
}

func newTestServiceWithEvents() (*Service, *fakeRepo, *fakeStore, *fakePublisher) {
	repo, store, pub := newFakeRepo(), &fakeStore{objects: map[string]ObjectInfo{}}, &fakePublisher{}
	svc := NewService(repo, store, pub, &fakeViews{seen: map[string]bool{}}, Options{
		MaxUploadBytes: 10 << 20,
		UploadURLTTL:   15 * time.Minute,
		PlaybackURLTTL: time.Hour,
		MediaBaseURL:   "http://media.test/videos",
	})
	return svc, repo, store, pub
}

// uploadedVideo membuat video milik user 7 yang sudah berstatus UPLOADED.
func uploadedVideo(t *testing.T, svc *Service, store *fakeStore) Video {
	t.Helper()
	ctx := context.Background()
	up, err := svc.CreateUpload(ctx, validInput())
	require.NoError(t, err)
	store.objects[up.Video.ObjectKey] = ObjectInfo{Size: 1234, ContentType: "video/mp4"}
	v, err := svc.CompleteUpload(ctx, 7, up.Video.ID)
	require.NoError(t, err)
	return v
}

func validInput() CreateUploadInput {
	return CreateUploadInput{OwnerID: 7, Title: "  Video pertama  ", Description: "halo", ContentType: "Video/MP4", SizeBytes: 1234}
}

func TestCreateUpload(t *testing.T) {
	svc, _, _ := newTestService()

	up, err := svc.CreateUpload(context.Background(), validInput())
	require.NoError(t, err)

	assert.Equal(t, "Video pertama", up.Video.Title, "judul dirapikan")
	assert.Equal(t, "video/mp4", up.Video.ContentType, "tipe dijadikan huruf kecil")
	assert.Equal(t, StatusPendingUpload, up.Video.Status)
	assert.Regexp(t, `^raw/[0-9a-f]{32}/source$`, up.Video.ObjectKey)
	assert.Contains(t, up.Request.URL, up.Video.ObjectKey)
	assert.Equal(t, "video/mp4", up.Request.Headers["Content-Type"])
}

func TestCreateUploadValidation(t *testing.T) {
	tests := map[string]func(*CreateUploadInput){
		"judul kosong":          func(in *CreateUploadInput) { in.Title = "   " },
		"judul terlalu panjang": func(in *CreateUploadInput) { in.Title = strings.Repeat("a", 101) },
		"deskripsi kepanjangan": func(in *CreateUploadInput) { in.Description = strings.Repeat("é", 1001) },
		"tipe bukan video":      func(in *CreateUploadInput) { in.ContentType = "image/png" },
		"ukuran nol":            func(in *CreateUploadInput) { in.SizeBytes = 0 },
		"ukuran melebihi batas": func(in *CreateUploadInput) { in.SizeBytes = 10<<20 + 1 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			svc, _, _ := newTestService()
			in := validInput()
			mutate(&in)
			_, err := svc.CreateUpload(context.Background(), in)
			var verr *ValidationError
			assert.ErrorAs(t, err, &verr)
		})
	}
}

func TestCompleteUpload(t *testing.T) {
	ctx := context.Background()
	svc, _, store, pub := newTestServiceWithEvents()
	up, err := svc.CreateUpload(ctx, validInput())
	require.NoError(t, err)
	id := up.Video.ID

	// File belum diunggah.
	_, err = svc.CompleteUpload(ctx, 7, id)
	assert.ErrorIs(t, err, ErrUploadIncomplete)

	// Ukuran file tidak sama dengan yang didaftarkan.
	store.objects[up.Video.ObjectKey] = ObjectInfo{Size: 99, ContentType: "video/mp4"}
	_, err = svc.CompleteUpload(ctx, 7, id)
	assert.ErrorIs(t, err, ErrUploadIncomplete)

	// User lain tidak boleh menyelesaikan upload milik orang lain.
	store.objects[up.Video.ObjectKey] = ObjectInfo{Size: 1234, ContentType: "video/mp4"}
	_, err = svc.CompleteUpload(ctx, 8, id)
	assert.ErrorIs(t, err, ErrNotFound)

	assert.Empty(t, pub.published, "belum ada event selama upload belum selesai")

	v, err := svc.CompleteUpload(ctx, 7, id)
	require.NoError(t, err)
	assert.Equal(t, StatusUploaded, v.Status)
	require.NotNil(t, v.UploadedAt)
	assert.Equal(t, []int64{id}, pub.published, "event videos.uploaded terkirim")

	// Memanggil ulang tetap berhasil (idempotent) dan mengirim event lagi;
	// JetStream yang membuang duplikatnya.
	again, err := svc.CompleteUpload(ctx, 7, id)
	require.NoError(t, err)
	assert.Equal(t, v.UploadedAt, again.UploadedAt)
	assert.Equal(t, []int64{id, id}, pub.published)

	// Setelah transcoder mulai bekerja, tidak ada event baru.
	require.NoError(t, svc.MarkProcessing(ctx, id))
	again, err = svc.CompleteUpload(ctx, 7, id)
	require.NoError(t, err)
	assert.Equal(t, StatusProcessing, again.Status)
	assert.Len(t, pub.published, 2)

	_, err = svc.CompleteUpload(ctx, 7, 999)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestCompleteUploadEventsUnavailable(t *testing.T) {
	ctx := context.Background()
	svc, _, store, pub := newTestServiceWithEvents()
	up, err := svc.CreateUpload(ctx, validInput())
	require.NoError(t, err)
	store.objects[up.Video.ObjectKey] = ObjectInfo{Size: 1234, ContentType: "video/mp4"}

	// NATS mati: client mendapat error yang boleh dicoba lagi...
	pub.err = errors.New("nats: no responders")
	_, err = svc.CompleteUpload(ctx, 7, up.Video.ID)
	assert.ErrorIs(t, err, ErrEventsUnavailable)

	// ...dan saat dicoba lagi setelah NATS hidup, event akhirnya terkirim.
	pub.err = nil
	v, err := svc.CompleteUpload(ctx, 7, up.Video.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusUploaded, v.Status)
	assert.Equal(t, []int64{up.Video.ID}, pub.published)
}

func TestTranscodeStatusFlow(t *testing.T) {
	ctx := context.Background()
	svc, repo, store := newTestService()
	id := uploadedVideo(t, svc, store).ID
	result := TranscodeResult{PlaylistKey: "hls/1/master.m3u8", ThumbnailKey: "hls/1/thumbnail.jpg", DurationMs: 5000, Renditions: []string{"360p"}}

	require.NoError(t, svc.MarkProcessing(ctx, id))
	assert.Equal(t, StatusProcessing, repo.videos[id].Status)

	// Hasil tanpa playlist ditolak (pesan rusak dari transcoder).
	var verr *ValidationError
	assert.ErrorAs(t, svc.MarkReady(ctx, id, TranscodeResult{}), &verr)

	require.NoError(t, svc.MarkReady(ctx, id, result))
	v := repo.videos[id]
	assert.Equal(t, StatusReady, v.Status)
	assert.Equal(t, int64(5000), v.DurationMs)
	assert.Equal(t, []string{"360p"}, v.Renditions)

	// Event yang datang terlambat atau dobel tidak mengubah video READY.
	require.NoError(t, svc.MarkProcessing(ctx, id))
	require.NoError(t, svc.MarkFailed(ctx, id, "telat"))
	assert.Equal(t, StatusReady, repo.videos[id].Status)

	assert.ErrorIs(t, svc.MarkProcessing(ctx, 999), ErrNotFound)
}

func TestMarkFailed(t *testing.T) {
	ctx := context.Background()
	svc, repo, store := newTestService()
	id := uploadedVideo(t, svc, store).ID

	require.NoError(t, svc.MarkFailed(ctx, id, strings.Repeat("x", 600)))
	v := repo.videos[id]
	assert.Equal(t, StatusFailed, v.Status)
	assert.Len(t, v.FailureReason, 500, "alasan dipotong sesuai ukuran kolom")
	require.NotNil(t, v.ProcessedAt)
}

func TestGetVideoVisibility(t *testing.T) {
	ctx := context.Background()
	svc, _, store := newTestService()
	up, err := svc.CreateUpload(ctx, validInput())
	require.NoError(t, err)
	id := up.Video.ID

	// Selama PENDING_UPLOAD: hanya pemilik yang bisa melihat, tanpa URL file.
	d, err := svc.GetVideo(ctx, 7, id)
	require.NoError(t, err)
	assert.Empty(t, d.Original.URL)
	_, err = svc.GetVideo(ctx, 8, id)
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = svc.GetVideo(ctx, 0, id)
	assert.ErrorIs(t, err, ErrNotFound)

	store.objects[up.Video.ObjectKey] = ObjectInfo{Size: 1234, ContentType: "video/mp4"}
	_, err = svc.CompleteUpload(ctx, 7, id)
	require.NoError(t, err)

	// UPLOADED dan PROCESSING: pemilik bisa melihat (dengan URL file asli),
	// orang lain belum.
	d, err = svc.GetVideo(ctx, 7, id)
	require.NoError(t, err)
	assert.Contains(t, d.Original.URL, "sig=get")
	_, err = svc.GetVideo(ctx, 0, id)
	assert.ErrorIs(t, err, ErrNotFound)

	// READY: siapa pun bisa melihat, termasuk yang belum login, tapi file
	// asli tetap hanya untuk pemilik.
	require.NoError(t, svc.MarkReady(ctx, id, TranscodeResult{PlaylistKey: "hls/1/master.m3u8", DurationMs: 1000, Renditions: []string{"360p"}}))
	d, err = svc.GetVideo(ctx, 0, id)
	require.NoError(t, err)
	assert.Equal(t, StatusReady, d.Video.Status)
	assert.Empty(t, d.Original.URL, "file asli tidak untuk umum")
	d, err = svc.GetVideo(ctx, 8, id)
	require.NoError(t, err)
	assert.Empty(t, d.Original.URL)
	d, err = svc.GetVideo(ctx, 7, id)
	require.NoError(t, err)
	assert.Contains(t, d.Original.URL, "sig=get")
}

func TestMediaURL(t *testing.T) {
	svc, _, _ := newTestService()

	assert.Equal(t, "http://media.test/videos/hls/42/master.m3u8", svc.MediaURL("hls/42/master.m3u8"))
	assert.Empty(t, svc.MediaURL(""), "video belum READY")
	assert.Empty(t, svc.MediaURL("raw/abc/source"), "file mentah tidak pernah publik")
}

func TestListMyVideosPagination(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService()
	for range 5 {
		_, err := svc.CreateUpload(ctx, validInput())
		require.NoError(t, err)
	}
	other := validInput()
	other.OwnerID = 8
	_, err := svc.CreateUpload(ctx, other)
	require.NoError(t, err)

	page1, next, err := svc.ListMyVideos(ctx, 7, 2, "")
	require.NoError(t, err)
	assert.Equal(t, []int64{5, 4}, ids(page1))
	assert.Equal(t, "4", next)

	page2, next, err := svc.ListMyVideos(ctx, 7, 2, next)
	require.NoError(t, err)
	assert.Equal(t, []int64{3, 2}, ids(page2))

	page3, next, err := svc.ListMyVideos(ctx, 7, 2, next)
	require.NoError(t, err)
	assert.Equal(t, []int64{1}, ids(page3))
	assert.Empty(t, next, "halaman terakhir")

	_, _, err = svc.ListMyVideos(ctx, 7, 2, "bukan-angka")
	var verr *ValidationError
	assert.ErrorAs(t, err, &verr)
	_, _, err = svc.ListMyVideos(ctx, 7, 51, "")
	assert.ErrorAs(t, err, &verr)
}

func ids(videos []Video) []int64 {
	out := make([]int64, len(videos))
	for i, v := range videos {
		out[i] = v.ID
	}
	return out
}
