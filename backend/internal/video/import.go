package video

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// Langkah 27: rekaman siaran langsung menjadi video.
//
// RecordingImporter (internal/livestream) mengambil rekaman dari server
// playback MediaMTX lalu memanggil ImportRecording dengan isinya. Hasilnya
// video DRAF milik kreator, yang lalu diproses transcoder seperti video
// unggahan biasa. Kreator bisa mengedit judulnya dan menerbitkannya dari
// Studio (draf tidak tampil di daftar mana pun sebelum diterbitkan).

// recordingContentType: MediaMTX mengirim rekaman sebagai fragmented MP4
// (format=fmp4), yang dibaca ffmpeg sama seperti MP4 biasa.
const recordingContentType = "video/mp4"

// ImportRecordingInput adalah isi ImportRecording.
type ImportRecordingInput struct {
	OwnerID     int64
	Title       string
	Description string
	Category    string
	// Body dibaca sampai habis lalu disimpan ke storage tanpa ditampung
	// seluruhnya di memori.
	Body io.Reader
}

// ErrRecordingEmpty: rekaman tidak berisi apa pun.
var ErrRecordingEmpty = errors.New("rekaman kosong")

// ImportRecording menyimpan rekaman ke storage dan membuat video draf
// berstatus UPLOADED. Event ke transcoder BELUM dikirim: pemanggil
// menyimpan id videonya dulu, lalu memanggil CompleteUpload (yang aman
// diulang). Dengan urutan ini, jika pod mati di tengah jalan, percobaan
// berikutnya tidak membuat video dobel.
func (s *Service) ImportRecording(ctx context.Context, in ImportRecordingInput) (Video, error) {
	title, err := cleanTitle(in.Title)
	if err != nil {
		return Video{}, err
	}
	desc, err := cleanDescription(in.Description)
	if err != nil {
		return Video{}, err
	}
	category, err := cleanCategory(in.Category)
	if err != nil {
		return Video{}, err
	}
	key, err := newObjectKey()
	if err != nil {
		return Video{}, err
	}
	// Baca satu byte lebih dari batas untuk tahu apakah rekamannya terlalu
	// besar, tanpa harus membaca semuanya.
	size, err := s.store.PutStream(ctx, key, recordingContentType, io.LimitReader(in.Body, s.opts.MaxUploadBytes+1))
	if err != nil {
		return Video{}, err
	}
	fail := func(err error) (Video, error) {
		// File yatim di raw/ dihapus; jika gagal, hanya dicatat pemanggil.
		if derr := s.store.Delete(context.WithoutCancel(ctx), key); derr != nil {
			return Video{}, fmt.Errorf("%w (file %s juga gagal dihapus: %v)", err, key, derr)
		}
		return Video{}, err
	}
	switch {
	case size == 0:
		return fail(ErrRecordingEmpty)
	case size > s.opts.MaxUploadBytes:
		return fail(fmt.Errorf("rekaman lebih besar dari batas unggah %d MB", s.opts.MaxUploadBytes>>20))
	}
	v, err := s.repo.Create(ctx, NewVideo{
		OwnerID:     in.OwnerID,
		Title:       title,
		Description: desc,
		ObjectKey:   key,
		ContentType: recordingContentType,
		SizeBytes:   size,
		Category:    category,
		Visibility:  VisibilityPublic,
		Draft:       true,
		SearchTerms: BuildSearchTerms(title, desc, nil),
	})
	if err != nil {
		return fail(err)
	}
	return s.repo.MarkUploaded(ctx, v.ID, s.now())
}
