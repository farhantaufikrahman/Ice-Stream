package video

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

// Versi palsu method notifikasi di Repository. Perilakunya meniru
// notification_repository.go: notifikasi untuk video yang tidak READY
// (atau sudah dihapus, artinya tidak ada di r.videos) tidak terlihat.

type fakeNotif struct {
	id, userID, videoID, actorID int64
	kind                         string
	createdAt                    time.Time
	readAt                       *time.Time
}

func (r *fakeRepo) InsertNotifications(_ context.Context, kind string, videoID, actorID int64, userIDs []int64, now time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var added int64
	for _, uid := range userIDs {
		if slices.ContainsFunc(r.notifs, func(n fakeNotif) bool { return n.userID == uid && n.videoID == videoID && n.kind == kind }) {
			continue
		}
		r.notifs = append(r.notifs, fakeNotif{id: int64(len(r.notifs) + 1), userID: uid, videoID: videoID, actorID: actorID, kind: kind, createdAt: now})
		added++
	}
	return added, nil
}

// visible mengembalikan notifikasi userID yang terlihat, id terbesar dulu.
func (r *fakeRepo) visible(userID int64) []Notification {
	var out []Notification
	for i := len(r.notifs) - 1; i >= 0; i-- {
		n := r.notifs[i]
		v, ok := r.videos[n.videoID]
		if n.userID != userID || !ok || v.Status != StatusReady {
			continue
		}
		out = append(out, Notification{ID: n.id, Kind: n.kind, Video: v, CreatedAt: n.createdAt, ReadAt: n.readAt})
	}
	return out
}

func (r *fakeRepo) ListNotifications(_ context.Context, userID, beforeID int64, limit int) ([]Notification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Notification
	for _, n := range r.visible(userID) {
		if (beforeID == 0 || n.ID < beforeID) && len(out) < limit {
			out = append(out, n)
		}
	}
	return out, nil
}

func (r *fakeRepo) CountUnreadNotifications(_ context.Context, userID int64, limit int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int64
	for _, x := range r.visible(userID) {
		if x.ReadAt == nil && n < int64(limit) {
			n++
		}
	}
	return n, nil
}

func (r *fakeRepo) MarkNotificationsRead(_ context.Context, userID, notificationID int64, now time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var changed int64
	for i, n := range r.notifs {
		if n.userID == userID && n.readAt == nil && (notificationID == 0 || n.id == notificationID) {
			r.notifs[i].readAt = &now
			changed++
		}
	}
	return changed, nil
}

// fakeSubscribers menggantikan UserService. subs[channelID] = id
// subscriber. err diisi test untuk mensimulasikan UserService yang mati.
type fakeSubscribers struct {
	mu    sync.Mutex
	subs  map[int64][]int64
	err   error
	calls int
}

func (f *fakeSubscribers) ListSubscriberIDs(_ context.Context, channelID, afterID int64, limit int) ([]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	var out []int64
	for _, id := range f.subs[channelID] { // sudah urut naik
		if id > afterID && len(out) < limit {
			out = append(out, id)
		}
	}
	return out, nil
}

// subscriberIDs membuat id from, from+1, ..., from+n-1.
func subscriberIDs(from, n int64) []int64 {
	ids := make([]int64, n)
	for i := range ids {
		ids[i] = from + int64(i)
	}
	return ids
}

func TestMarkReadyPublishes(t *testing.T) {
	ctx := context.Background()
	svc, repo, store, pub := newTestServiceWithEvents()
	result := TranscodeResult{PlaylistKey: "hls/1/master.m3u8", DurationMs: 5000, Renditions: []string{"360p"}}

	// NATS mati sesudah status berubah: error (supaya event transcode
	// dicoba lagi), tapi status tetap READY.
	v := uploadedVideo(t, svc, store)
	pub.err = errors.New("nats mati")
	assert.ErrorIs(t, svc.MarkReady(ctx, v.ID, result), ErrEventsUnavailable)
	assert.Equal(t, StatusReady, repo.videos[v.ID].Status)
	assert.Empty(t, pub.ready)

	// Percobaan ulang: status sudah READY, event tetap dikirim.
	pub.err = nil
	require.NoError(t, svc.MarkReady(ctx, v.ID, result))
	assert.Equal(t, []int64{v.ID}, pub.ready)

	// Video FAILED tidak pernah dipublikasikan.
	failed := uploadedVideo(t, svc, store)
	require.NoError(t, svc.MarkFailed(ctx, failed.ID, "rusak"))
	require.NoError(t, svc.MarkReady(ctx, failed.ID, result))
	assert.Equal(t, []int64{v.ID}, pub.ready)
}

func TestNotifyNewVideo(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	// Channel 7 punya 2501 subscriber: tiga halaman UserService (1000,
	// 1000, 501) dan 26 kali INSERT (per 100).
	subs := &fakeSubscribers{subs: map[int64][]int64{7: subscriberIDs(100, 2501), 8: {7}}}
	notifier := NewNotifier(repo, subs)
	v := readyVideo(repo, 7, time.Now())

	pages := 0
	n, err := notifier.NotifyNewVideo(ctx, v.ID, func() { pages++ })
	require.NoError(t, err)
	assert.Equal(t, int64(2501), n)
	assert.Equal(t, 3, pages)
	assert.Equal(t, 3, subs.calls)

	// Event dobel: tidak ada notifikasi baru.
	n, err = notifier.NotifyNewVideo(ctx, v.ID, nil)
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Len(t, repo.notifs, 2501)

	first := repo.notifs[0]
	assert.Equal(t, NotificationNewVideo, first.kind)
	assert.Equal(t, int64(7), first.actorID, "actor = pemilik channel")

	// Subscriber 100 melihat notifikasinya.
	items, _, err := svc.ListNotifications(ctx, 100, 0, "")
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, v.ID, items[0].Video.ID)

	t.Run("UserService mati", func(t *testing.T) {
		other := readyVideo(repo, 8, time.Now())
		subs.err = errors.New("unavailable")
		_, err := notifier.NotifyNewVideo(ctx, other.ID, nil)
		assert.Error(t, err)
		subs.err = nil
		n, err := notifier.NotifyNewVideo(ctx, other.ID, nil)
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)
	})
	t.Run("video sudah dihapus atau belum READY", func(t *testing.T) {
		calls := subs.calls
		n, err := notifier.NotifyNewVideo(ctx, 9999, nil)
		require.NoError(t, err)
		assert.Zero(t, n)
		up, err := svc.CreateUpload(ctx, validInput())
		require.NoError(t, err)
		n, err = notifier.NotifyNewVideo(ctx, up.Video.ID, nil)
		require.NoError(t, err)
		assert.Zero(t, n)
		assert.Equal(t, calls, subs.calls, "UserService tidak dipanggil")
	})
}

func TestNotificationInbox(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	subs := &fakeSubscribers{subs: map[int64][]int64{7: {20, 21}}}
	notifier := NewNotifier(repo, subs)
	var videos []Video
	for range 5 {
		v := readyVideo(repo, 7, time.Now())
		_, err := notifier.NotifyNewVideo(ctx, v.ID, nil)
		require.NoError(t, err)
		videos = append(videos, v)
	}

	// Halaman 1 dan 2, terbaru lebih dulu.
	page1, next, err := svc.ListNotifications(ctx, 20, 3, "")
	require.NoError(t, err)
	require.NotEmpty(t, next)
	page2, next2, err := svc.ListNotifications(ctx, 20, 3, next)
	require.NoError(t, err)
	assert.Empty(t, next2)
	var got []int64
	for _, n := range append(page1, page2...) {
		got = append(got, n.Video.ID)
	}
	assert.Equal(t, []int64{videos[4].ID, videos[3].ID, videos[2].ID, videos[1].ID, videos[0].ID}, got)

	unread, err := svc.CountUnreadNotifications(ctx, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(5), unread)

	// Tandai satu, lalu tandai lagi (aman diulang).
	r, err := svc.MarkNotificationsRead(ctx, 20, page1[0].ID)
	require.NoError(t, err)
	assert.Equal(t, ReadResult{Updated: 1, UnreadCount: 4}, r)
	r, err = svc.MarkNotificationsRead(ctx, 20, page1[0].ID)
	require.NoError(t, err)
	assert.Equal(t, ReadResult{Updated: 0, UnreadCount: 4}, r)

	// Notifikasi milik user 20 tidak bisa ditandai oleh user 21.
	r, err = svc.MarkNotificationsRead(ctx, 21, page1[1].ID)
	require.NoError(t, err)
	assert.Equal(t, ReadResult{Updated: 0, UnreadCount: 5}, r)

	// Video dihapus: notifikasinya hilang dari daftar dan hitungan.
	_, err = svc.DeleteVideo(ctx, 7, videos[0].ID)
	require.NoError(t, err)
	unread, err = svc.CountUnreadNotifications(ctx, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(3), unread)

	// Tandai semua.
	r, err = svc.MarkNotificationsRead(ctx, 20, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(0), r.UnreadCount)
	items, _, err := svc.ListNotifications(ctx, 20, 0, "")
	require.NoError(t, err)
	assert.Len(t, items, 4)
	for _, n := range items {
		assert.NotNil(t, n.ReadAt)
	}

	t.Run("validasi", func(t *testing.T) {
		var verr *ValidationError
		_, _, err := svc.ListNotifications(ctx, 20, 51, "")
		assert.ErrorAs(t, err, &verr)
		_, _, err = svc.ListNotifications(ctx, 20, 0, encodeHistoryCursor(HistoryCursor{WatchedAt: time.Now(), VideoID: 1}))
		assert.ErrorAs(t, err, &verr, "token riwayat bukan token notifikasi")
		_, _, err = svc.ListNotifications(ctx, 20, 0, "!!")
		assert.ErrorAs(t, err, &verr)
		_, err = svc.MarkNotificationsRead(ctx, 20, -1)
		assert.ErrorIs(t, err, ErrNotFound)
		_, err = svc.CountUnreadNotifications(ctx, 0)
		assert.Error(t, err)
	})
}

func TestCountUnreadCapped(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	notifier := NewNotifier(repo, &fakeSubscribers{subs: map[int64][]int64{7: {20}}})
	for range maxUnreadCount + 5 {
		v := readyVideo(repo, 7, time.Now())
		_, err := notifier.NotifyNewVideo(ctx, v.ID, nil)
		require.NoError(t, err)
	}
	n, err := svc.CountUnreadNotifications(ctx, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(maxUnreadCount), n)
}

func TestNotificationGRPC(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	srv := NewGRPCServer(svc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	v := readyVideo(repo, 7, time.Now())
	repo.mu.Lock()
	v.ThumbnailKey = "hls/1/thumbnail.jpg"
	repo.videos[v.ID] = v
	repo.mu.Unlock()
	_, err := NewNotifier(repo, &fakeSubscribers{subs: map[int64][]int64{7: {20}}}).NotifyNewVideo(ctx, v.ID, nil)
	require.NoError(t, err)

	list, err := srv.ListNotifications(ctx, &videov1.ListNotificationsRequest{RequesterId: 20})
	require.NoError(t, err)
	require.Len(t, list.GetNotifications(), 1)
	n := list.GetNotifications()[0]
	assert.Equal(t, NotificationNewVideo, n.GetKind())
	assert.Equal(t, v.ID, n.GetVideo().GetId())
	assert.Equal(t, "http://media.test/videos/hls/1/thumbnail.jpg", n.GetVideo().GetThumbnailUrl())
	assert.Nil(t, n.GetReadAt())

	count, err := srv.CountUnreadNotifications(ctx, &videov1.CountUnreadNotificationsRequest{RequesterId: 20})
	require.NoError(t, err)
	assert.Equal(t, int64(1), count.GetUnreadCount())

	read, err := srv.MarkNotificationsRead(ctx, &videov1.MarkNotificationsReadRequest{RequesterId: 20, NotificationId: n.GetId()})
	require.NoError(t, err)
	assert.Equal(t, int64(1), read.GetUpdated())
	assert.Zero(t, read.GetUnreadCount())

	list, err = srv.ListNotifications(ctx, &videov1.ListNotificationsRequest{RequesterId: 20})
	require.NoError(t, err)
	assert.NotNil(t, list.GetNotifications()[0].GetReadAt())

	_, err = srv.ListNotifications(ctx, &videov1.ListNotificationsRequest{RequesterId: 20, PageSize: 51})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestChunks(t *testing.T) {
	var got []string
	for c := range chunks(subscriberIDs(1, 7), 3) {
		got = append(got, fmt.Sprint(c))
	}
	assert.Equal(t, []string{"[1 2 3]", "[4 5 6]", "[7]"}, got)
	for range chunks(nil, 3) {
		t.Fatal("tidak boleh ada potongan untuk daftar kosong")
	}
}
