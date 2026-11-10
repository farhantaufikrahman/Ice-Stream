package video

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Notifikasi video baru.
//
// Alurnya (lihat juga migrasi V11):
//
//  1. Transcoder selesai, MarkReady mengubah status video menjadi READY
//     lalu mengirim event videos.published ke NATS JetStream.
//  2. Notifier (consumer video-notify) menerima event itu, meminta daftar
//     subscriber channel ke UserService halaman demi halaman, lalu
//     menyimpan satu notifikasi untuk setiap subscriber.
//  3. User membuka daftar notifikasi (ListNotifications), melihat jumlah
//     yang belum dibaca (CountUnreadNotifications), dan menandainya sudah
//     dibaca (MarkNotificationsRead).
//
// Langkah 2 berjalan di background, jadi MarkReady tidak perlu menunggu
// ribuan baris tersimpan. Kalau UserService atau Oracle sedang mati,
// JetStream mengirim ulang event-nya nanti.

const (
	// subscriberPageSize adalah jumlah id subscriber yang diminta ke
	// UserService dalam satu panggilan (batas ListSubscriberIds).
	subscriberPageSize = 1000
	// notificationInsertBatch adalah jumlah notifikasi dalam satu INSERT.
	// Id subscriber dikirim ke Oracle sebagai teks JSON; 100 id paling
	// panjang sekitar 2 KB, aman di bawah batas 4000 byte VARCHAR2.
	notificationInsertBatch = 100
	// maxUnreadCount: jumlah yang belum dibaca dihitung sampai 100 saja.
	// Badge cukup menampilkan "99+"; menghitung lebih dari itu buang waktu.
	maxUnreadCount = 100
)

// SubscriberSource mengambil id subscriber sebuah channel. Implementasi
// gRPC (UserService) ada di subscribers.go; test memakai versi palsu.
type SubscriberSource interface {
	// ListSubscriberIDs mengambil maksimal limit id subscriber channelID
	// yang lebih besar dari afterID, urut dari yang terkecil.
	ListSubscriberIDs(ctx context.Context, channelID, afterID int64, limit int) ([]int64, error)
}

// Notifier membuat notifikasi NEW_VIDEO untuk semua subscriber channel.
type Notifier struct {
	repo Repository
	subs SubscriberSource
	now  func() time.Time
}

// NewNotifier membuat Notifier.
func NewNotifier(repo Repository, subs SubscriberSource) *Notifier {
	return &Notifier{repo: repo, subs: subs, now: func() time.Time { return time.Now().UTC() }}
}

// NotifyNewVideo menyimpan notifikasi NEW_VIDEO untuk setiap subscriber
// pemilik videoID dan mengembalikan jumlah notifikasi baru. progress
// (boleh nil) dipanggil setelah setiap halaman subscriber selesai, supaya
// consumer bisa memberi tahu JetStream bahwa pesannya masih dikerjakan.
//
// Aman dipanggil berulang untuk video yang sama: subscriber yang sudah
// punya notifikasinya dilewati (lihat InsertNotifications). Jadi kalau
// proses ini berhenti di tengah, cukup diulang dari awal.
func (n *Notifier) NotifyNewVideo(ctx context.Context, videoID int64, progress func()) (int64, error) {
	v, err := n.repo.FindByID(ctx, videoID)
	if errors.Is(err, ErrNotFound) {
		// Video sudah dihapus sebelum notifikasinya sempat dibuat.
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if v.Status != StatusReady {
		return 0, nil
	}
	now := n.now()
	var total, after int64
	for {
		ids, err := n.subs.ListSubscriberIDs(ctx, v.OwnerID, after, subscriberPageSize)
		if err != nil {
			return total, fmt.Errorf("list subscriber channel %d: %w", v.OwnerID, err)
		}
		for batch := range chunks(ids, notificationInsertBatch) {
			added, err := n.repo.InsertNotifications(ctx, NotificationNewVideo, v.ID, v.OwnerID, batch, now)
			total += added
			if err != nil {
				return total, err
			}
		}
		if progress != nil {
			progress()
		}
		if len(ids) < subscriberPageSize {
			return total, nil
		}
		after = ids[len(ids)-1]
	}
}

// chunks membagi ids menjadi potongan berukuran maksimal size.
func chunks(ids []int64, size int) func(yield func([]int64) bool) {
	return func(yield func([]int64) bool) {
		for len(ids) > 0 {
			n := min(size, len(ids))
			if !yield(ids[:n]) {
				return
			}
			ids = ids[n:]
		}
	}
}

// ListNotifications mengambil notifikasi requesterID, terbaru lebih dulu.
// Notifikasi untuk video yang sudah dihapus atau tidak READY lagi tidak
// ikut ditampilkan.
func (s *Service) ListNotifications(ctx context.Context, requesterID int64, pageSize int, pageToken string) ([]Notification, string, error) {
	if requesterID <= 0 {
		// Gateway selalu mengisi requester dari access token, jadi ini bug.
		return nil, "", errors.New("requester id kosong")
	}
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		return nil, "", &ValidationError{"page_size", fmt.Sprintf("maksimal %d", maxPageSize)}
	}
	var beforeID int64
	if pageToken != "" {
		id, err := decodeNotificationCursor(pageToken)
		if err != nil {
			return nil, "", &ValidationError{"page_token", "tidak valid"}
		}
		beforeID = id
	}
	// Ambil satu lebih banyak untuk tahu apakah masih ada halaman berikutnya.
	items, err := s.repo.ListNotifications(ctx, requesterID, beforeID, pageSize+1)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(items) > pageSize {
		items = items[:pageSize]
		next = encodeNotificationCursor(items[pageSize-1].ID)
	}
	return items, next, nil
}

// CountUnreadNotifications menghitung notifikasi requesterID yang belum
// dibaca, maksimal maxUnreadCount.
func (s *Service) CountUnreadNotifications(ctx context.Context, requesterID int64) (int64, error) {
	if requesterID <= 0 {
		return 0, errors.New("requester id kosong")
	}
	return s.repo.CountUnreadNotifications(ctx, requesterID, maxUnreadCount)
}

// ReadResult adalah hasil MarkNotificationsRead.
type ReadResult struct {
	// Updated adalah jumlah notifikasi yang baru ditandai dibaca.
	Updated int64
	// UnreadCount adalah sisa yang belum dibaca, untuk memperbarui badge.
	UnreadCount int64
}

// MarkNotificationsRead menandai satu notifikasi (notificationID > 0) atau
// semua notifikasi (notificationID = 0) milik requesterID sebagai sudah
// dibaca. Aman dipanggil berulang. Notifikasi milik orang lain tidak
// berubah dan tidak dianggap error, sama seperti DeleteWatchHistory.
func (s *Service) MarkNotificationsRead(ctx context.Context, requesterID, notificationID int64) (ReadResult, error) {
	if requesterID <= 0 {
		return ReadResult{}, errors.New("requester id kosong")
	}
	if notificationID < 0 {
		return ReadResult{}, ErrNotFound
	}
	n, err := s.repo.MarkNotificationsRead(ctx, requesterID, notificationID, s.now())
	if err != nil {
		return ReadResult{}, err
	}
	unread, err := s.repo.CountUnreadNotifications(ctx, requesterID, maxUnreadCount)
	if err != nil {
		return ReadResult{}, err
	}
	return ReadResult{Updated: n, UnreadCount: unread}, nil
}

// Token halaman notifikasi: base64 dari "n<id notifikasi terakhir>". Huruf
// "n" membedakannya dari token feed, riwayat, dan pencarian.
func encodeNotificationCursor(id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte("n" + strconv.FormatInt(id, 10)))
}

func decodeNotificationCursor(token string) (int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, err
	}
	body, ok := strings.CutPrefix(string(raw), "n")
	if !ok {
		return 0, errors.New("bukan token notifikasi")
	}
	id, err := strconv.ParseInt(body, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("id cursor salah")
	}
	return id, nil
}
