package video

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
)

// Edit dan hapus video oleh pemiliknya.

// UpdateInput adalah isi permintaan edit. Field nil = tidak diubah, jadi
// client boleh mengirim judul saja atau deskripsi saja (seperti PATCH).
type UpdateInput struct {
	RequesterID int64
	VideoID     int64
	Title       *string
	Description *string
	// Category nil = tidak diubah, "" = hapus kategori.
	Category *string
	// Tags nil = tidak diubah, slice kosong = hapus semua tag.
	Tags []string
	// SetTags membedakan "tag tidak dikirim" dari "hapus semua tag": slice
	// kosong dan nil sama-sama berarti tanpa tag di Go.
	SetTags bool
	// Visibility nil = tidak diubah.
	Visibility *string
	// Draft nil = tidak diubah. false = "Terbitkan" video draf. Video yang
	// sudah terbit boleh dijadikan draf lagi (disembunyikan dari orang
	// lain), seperti YouTube Studio.
	Draft *bool
	// Language nil = tidak diubah, "" = hapus bahasa.
	Language *string
}

// UpdateVideo mengganti judul, deskripsi, kategori, tag, visibilitas,
// status draf, dan/atau bahasa video milik requester. Kata pencarian dibuat
// ulang dari data baru, dalam transaksi yang sama, supaya pencarian
// langsung memakai teks terbaru. Boleh untuk video dengan status apa pun.
//
// Jika video READY baru saja menjadi publik (misalnya draf diterbitkan,
// atau Pribadi diubah menjadi Publik), event videos.published dikirim
// supaya subscriber mendapat notifikasi. Subscriber yang sudah pernah
// diberi tahu tentang video ini tidak diberi tahu lagi (lihat Notifier).
func (s *Service) UpdateVideo(ctx context.Context, in UpdateInput) (Video, error) {
	if in.RequesterID <= 0 {
		// Gateway selalu mengisi requester dari access token, jadi ini bug.
		return Video{}, errors.New("requester id kosong")
	}
	if in.Title == nil && in.Description == nil && in.Category == nil && !in.SetTags &&
		in.Visibility == nil && in.Draft == nil && in.Language == nil {
		return Video{}, &ValidationError{"title", "isi minimal satu field yang diubah"}
	}
	if in.VideoID <= 0 {
		return Video{}, ErrNotFound
	}
	v, err := s.repo.FindByID(ctx, in.VideoID)
	if err != nil {
		return Video{}, err
	}
	if v.OwnerID != in.RequesterID {
		// Sama seperti CompleteUpload: orang lain tidak boleh tahu video
		// ini ada.
		return Video{}, ErrNotFound
	}
	d := Details{
		Title: v.Title, Description: v.Description, Category: v.Category, Tags: v.Tags,
		Visibility: v.Visibility, Draft: v.Draft, Language: v.Language,
	}
	if in.Title != nil {
		if d.Title, err = cleanTitle(*in.Title); err != nil {
			return Video{}, err
		}
	}
	if in.Description != nil {
		if d.Description, err = cleanDescription(*in.Description); err != nil {
			return Video{}, err
		}
	}
	if in.Category != nil {
		if d.Category, err = cleanCategory(*in.Category); err != nil {
			return Video{}, err
		}
	}
	if in.SetTags {
		if d.Tags, err = cleanTags(in.Tags); err != nil {
			return Video{}, err
		}
	}
	if in.Visibility != nil {
		if d.Visibility, err = cleanVideoVisibility(*in.Visibility); err != nil {
			return Video{}, err
		}
	}
	if in.Draft != nil {
		d.Draft = *in.Draft
	}
	if in.Language != nil {
		if d.Language, err = cleanLanguage(*in.Language); err != nil {
			return Video{}, err
		}
	}
	updated, err := s.repo.UpdateDetails(ctx, v.ID, in.RequesterID, d, BuildSearchTerms(d.Title, d.Description, d.Tags), s.now())
	if err != nil {
		return Video{}, err
	}
	if updated.IsListed() && !v.IsListed() {
		// Perubahan sudah tersimpan; kalau event gagal, yang hilang hanya
		// notifikasinya. Jangan gagalkan edit hanya karena NATS bermasalah.
		if err := s.events.PublishPublished(ctx, updated); err != nil {
			return updated, fmt.Errorf("%w: %w", ErrEventsUnavailable, err)
		}
	}
	return updated, nil
}

// deletableStatuses adalah status yang boleh dihapus. UPLOADED dan
// PROCESSING tidak termasuk: transcoder sedang (atau akan) menulis file HLS,
// dan file itu akan tertinggal di storage kalau videonya sudah dihapus.
var deletableStatuses = []string{StatusPendingUpload, StatusReady, StatusFailed}

// DeleteVideo menghapus video milik requester dari semua tempat: feed,
// pencarian, channel, riwayat, komentar, dan danmaku (soft delete, lihat
// migrasi V10). Video yang sudah dihapus dijawab ErrNotFound, sama seperti
// video yang tidak pernah ada.
//
// File di storage TIDAK dihapus di sini. Panggil PurgeMedia dengan video
// yang dikembalikan; kalau gagal, PurgeDeleted mencobanya lagi nanti.
// Dipisah supaya storage yang sedang mati tidak membuat penghapusan gagal:
// bagi user, videonya sudah hilang.
func (s *Service) DeleteVideo(ctx context.Context, requesterID, videoID int64) (Video, error) {
	if requesterID <= 0 {
		return Video{}, errors.New("requester id kosong")
	}
	if videoID <= 0 {
		return Video{}, ErrNotFound
	}
	v, err := s.repo.FindByID(ctx, videoID)
	if err != nil {
		return Video{}, err
	}
	if v.OwnerID != requesterID {
		return Video{}, ErrNotFound
	}
	// Status juga diperiksa ulang di dalam UPDATE (lihat SoftDelete), jadi
	// tetap benar walau status berubah di antara dua query ini.
	if err := s.repo.SoftDelete(ctx, v.ID, requesterID, deletableStatuses, s.now()); err != nil {
		return Video{}, err
	}
	return v, nil
}

// PurgeMedia menghapus semua file milik video yang sudah dihapus: file
// asli (raw/<acak>/) dan hasil transcoding (hls/<id>/). Mengembalikan jumlah
// file yang dihapus. Aman dipanggil berulang.
func (s *Service) PurgeMedia(ctx context.Context, v Video) (int, error) {
	total := 0
	for _, prefix := range mediaPrefixes(v) {
		n, err := s.store.DeletePrefix(ctx, prefix)
		total += n
		if err != nil {
			return total, err
		}
	}
	if err := s.repo.MarkPurged(ctx, v.ID, s.now()); err != nil {
		return total, err
	}
	return total, nil
}

// mediaPrefixes mengembalikan folder storage milik satu video, misalnya
// "raw/3f9c.../" dan "hls/42/".
func mediaPrefixes(v Video) []string {
	prefixes := make([]string, 0, 2)
	// ObjectKey berbentuk "raw/<32 huruf hex>/source" (lihat newObjectKey).
	// Hanya folder dengan bentuk itu yang dihapus, supaya data yang aneh
	// tidak pernah membuat kita menghapus "raw/" seluruhnya.
	if dir := path.Dir(v.ObjectKey); strings.HasPrefix(dir, "raw/") && len(dir) > len("raw/") {
		prefixes = append(prefixes, dir+"/")
	}
	if v.ID > 0 {
		prefixes = append(prefixes, PublicPrefix+strconv.FormatInt(v.ID, 10)+"/")
	}
	return prefixes
}

// PurgeDeleted menghapus file storage milik semua video yang sudah dihapus
// tapi filenya tertinggal (misalnya storage mati saat DeleteVideo).
// Dipanggil sekali saat VideoService start, di goroutine. Mengembalikan
// jumlah video yang dibersihkan.
func (s *Service) PurgeDeleted(ctx context.Context) (int, error) {
	total := 0
	for {
		videos, err := s.repo.ListUnpurged(ctx, 100)
		if err != nil {
			return total, err
		}
		if len(videos) == 0 {
			return total, nil
		}
		for _, v := range videos {
			if _, err := s.PurgeMedia(ctx, v); err != nil {
				return total, fmt.Errorf("video %d: %w", v.ID, err)
			}
			total++
		}
	}
}
