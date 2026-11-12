package livestream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/farhantaufikrahman/ice-stream/backend/internal/video"
)

// Langkah 27: "Simpan rekaman sebagai video setelah siaran selesai"
// (desain GoLive).
//
//	OBS/browser --> MediaMTX --tulis--> /recordings (disk pod MediaMTX)
//	                   ^ playback :9996 (/list, /get)
//	                   |
//	RecordingImporter (VideoService) --stream--> RustFS raw/... --> transcoder
//
// MediaMTX merekam SEMUA siaran ke disk-nya (record: yes di
// infra/k8s/live/configmap.yaml) dan menghapusnya setelah 1 hari. Saat
// siaran selesai dan pilihan rekaman kanal aktif, Sync menandai siaran itu
// PENDING. RecordingImporter lalu mengambil potongan rekamannya lewat
// server playback MediaMTX dan menyimpannya sebagai video draf.
//
// Dokumentasi server playback: https://mediamtx.org/docs/features/playback
//
// Padanannya: pekerja latar (BullMQ worker di Node.js, @Scheduled atau
// Spring Batch di Java) yang mengambil tugas dari tabel.

// PlaybackSpan adalah satu rentang rekaman tanpa jeda dari GET /list.
type PlaybackSpan struct {
	Start    time.Time
	Duration time.Duration
}

// End adalah akhir rentang.
func (p PlaybackSpan) End() time.Time { return p.Start.Add(p.Duration) }

// PlaybackClient memanggil server playback MediaMTX.
type PlaybackClient struct {
	// BaseURL misalnya "http://mediamtx:9996".
	BaseURL string
	// HTTP tanpa Timeout: unduhan rekaman bisa lama, batasnya dari ctx.
	HTTP *http.Client
}

// List membaca rentang rekaman path di antara start dan end. Rekaman yang
// tidak ada dijawab MediaMTX dengan 404; itu dianggap daftar kosong.
func (c PlaybackClient) List(ctx context.Context, path string, start, end time.Time) ([]PlaybackSpan, error) {
	q := url.Values{
		"path":  {path},
		"start": {start.UTC().Format(time.RFC3339Nano)},
		"end":   {end.UTC().Format(time.RFC3339Nano)},
	}
	resp, err := c.do(ctx, "/list?"+q.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MediaMTX playback /list menjawab HTTP %d", resp.StatusCode)
	}
	var raw []struct {
		Start time.Time `json:"start"`
		// Duration dalam detik (boleh pecahan).
		Duration float64 `json:"duration"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("baca jawaban /list: %w", err)
	}
	out := make([]PlaybackSpan, 0, len(raw))
	for _, r := range raw {
		out = append(out, PlaybackSpan{Start: r.Start.UTC(), Duration: time.Duration(r.Duration * float64(time.Second))})
	}
	return out, nil
}

// Get mengunduh rekaman path mulai start selama d sebagai fragmented MP4.
// Pemanggil wajib menutup hasilnya.
func (c PlaybackClient) Get(ctx context.Context, path string, start time.Time, d time.Duration) (io.ReadCloser, error) {
	q := url.Values{
		"path":     {path},
		"start":    {start.UTC().Format(time.RFC3339Nano)},
		"duration": {strconv.FormatFloat(d.Seconds(), 'f', 3, 64)},
		"format":   {"fmp4"},
	}
	resp, err := c.do(ctx, "/get?"+q.Encode())
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("MediaMTX playback /get menjawab HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}

func (c PlaybackClient) do(ctx context.Context, pathAndQuery string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+pathAndQuery, nil)
	if err != nil {
		return nil, err
	}
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("panggil MediaMTX playback: %w", err)
	}
	return resp, nil
}

// RecordingSource adalah bagian PlaybackClient yang dipakai importer.
type RecordingSource interface {
	List(ctx context.Context, path string, start, end time.Time) ([]PlaybackSpan, error)
	Get(ctx context.Context, path string, start time.Time, d time.Duration) (io.ReadCloser, error)
}

// RecordingSink adalah bagian video.Service yang dipakai importer.
type RecordingSink interface {
	ImportRecording(ctx context.Context, in video.ImportRecordingInput) (video.Video, error)
	CompleteUpload(ctx context.Context, ownerID, videoID int64) (video.Video, error)
}

const (
	// recordingMargin: rentang yang dicari sedikit lebih lebar dari
	// started_at..ended_at, karena ended_at adalah last_seen_at (Syncer
	// membaca MediaMTX setiap 5 detik). Siaran berikutnya milik kreator
	// yang sama tidak mungkin dimulai dalam rentang ini, karena siaran yang
	// kembali dalam batas tenggang (15 detik) dianggap siaran yang sama.
	recordingMargin = 10 * time.Second
	// recordingStaleAfter: impor yang diambil lebih lama dari ini dianggap
	// macet (pod mati) dan diambil lagi.
	recordingStaleAfter = 30 * time.Minute
)

// RecordingImporter mengimpor rekaman siaran yang menunggu, satu per satu.
type RecordingImporter struct {
	Repo     Repository
	Source   RecordingSource
	Sink     RecordingSink
	Interval time.Duration
	Logger   *slog.Logger
	// Now boleh nil (time.Now).
	Now func() time.Time
}

// Run berjalan sampai ctx dibatalkan.
func (im RecordingImporter) Run(ctx context.Context) {
	t := time.NewTicker(im.Interval)
	defer t.Stop()
	for {
		// Habiskan antrean dulu, baru tunggu putaran berikutnya.
		for {
			done, err := im.RunOnce(ctx)
			if err != nil {
				im.Logger.Warn("impor rekaman siaran gagal", slog.Any("error", err))
			}
			if !done || err != nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// RunOnce mengimpor satu rekaman. done false jika tidak ada yang menunggu.
//
// Urutannya dibuat supaya aman diulang:
//  1. ambil tugas (IMPORTING);
//  2. jika belum ada video: unduh dari MediaMTX, simpan ke storage, buat
//     video draf, lalu simpan id videonya di live_streams;
//  3. kirim event ke transcoder (CompleteUpload, aman diulang);
//  4. tandai DONE.
//
// Jika pod mati atau NATS mati sesudah langkah 2, tugas diambil lagi
// setelah recordingStaleAfter dan langsung lanjut ke langkah 3, jadi tidak
// ada video dobel. Kegagalan sebelum video dibuat (rekaman tidak ada,
// terlalu besar, MediaMTX tidak bisa dihubungi) ditandai FAILED beserta
// alasannya, yang ditampilkan di Studio Live.
func (im RecordingImporter) RunOnce(ctx context.Context) (bool, error) {
	now := time.Now().UTC()
	if im.Now != nil {
		now = im.Now()
	}
	st, err := im.Repo.ClaimRecording(ctx, now, now.Add(-recordingStaleAfter))
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	log := im.Logger.With(slog.Int64("stream_id", st.ID), slog.Int64("owner_id", st.OwnerID))

	videoID := st.RecordingVideoID
	if videoID == 0 {
		v, reason, err := im.importVideo(ctx, st)
		if err != nil {
			log.Warn("rekaman siaran tidak bisa diimpor", slog.String("reason", reason), slog.Any("error", err))
			if ferr := im.Repo.FailRecording(ctx, st.ID, reason); ferr != nil {
				return true, ferr
			}
			return true, nil
		}
		videoID = v.ID
		if err := im.Repo.SetRecordingVideo(ctx, st.ID, videoID); err != nil {
			return true, err
		}
	}
	_, err = im.Sink.CompleteUpload(ctx, st.OwnerID, videoID)
	switch {
	case errors.Is(err, video.ErrNotFound):
		// Kreator sudah menghapus videonya sebelum diproses.
		return true, im.Repo.FailRecording(ctx, st.ID, "video rekaman sudah dihapus")
	case err != nil:
		// Biasanya NATS sedang mati. Status tetap IMPORTING; tugas ini
		// diambil lagi setelah recordingStaleAfter.
		return true, fmt.Errorf("kirim rekaman %d ke transcoder: %w", st.ID, err)
	}
	log.Info("rekaman siaran menjadi video draf", slog.Int64("video_id", videoID))
	return true, im.Repo.FinishRecording(ctx, st.ID)
}

// importVideo mengunduh rentang rekaman terpanjang lalu menyimpannya
// sebagai video. reason adalah alasan singkat untuk kreator jika gagal.
func (im RecordingImporter) importVideo(ctx context.Context, st Stream) (video.Video, string, error) {
	path := PathFor(st.OwnerID)
	from, to := st.StartedAt.Add(-recordingMargin), st.EndedAt.Add(recordingMargin)
	spans, err := im.Source.List(ctx, path, from, to)
	if err != nil {
		return video.Video{}, "server rekaman tidak bisa dihubungi", err
	}
	span, ok := longestSpan(spans, from, to)
	if !ok {
		return video.Video{}, "rekaman tidak ditemukan (rekaman disimpan 1 hari)", errors.New("tidak ada rentang rekaman")
	}
	body, err := im.Source.Get(ctx, path, span.Start, span.Duration)
	if err != nil {
		return video.Video{}, "server rekaman tidak bisa dihubungi", err
	}
	defer body.Close()
	v, err := im.Sink.ImportRecording(ctx, video.ImportRecordingInput{
		OwnerID:     st.OwnerID,
		Title:       st.Title,
		Description: "Rekaman siaran langsung " + st.StartedAt.In(wib).Format("02/01/2006 pukul 15.04") + " WIB.",
		Category:    st.Category,
		Body:        body,
	})
	if err != nil {
		var verr *video.ValidationError
		switch {
		case errors.Is(err, video.ErrRecordingEmpty):
			return video.Video{}, "rekaman kosong", err
		case errors.As(err, &verr):
			return video.Video{}, verr.Error(), err
		}
		return video.Video{}, "rekaman gagal disimpan", err
	}
	return v, "", nil
}

// wib: waktu di deskripsi rekaman. Ice-Stream ditujukan untuk penonton di
// Indonesia bagian barat; zona tetap dipakai supaya tidak bergantung pada
// data zona waktu di image.
var wib = time.FixedZone("WIB", 7*60*60)

// longestSpan memilih rentang rekaman terpanjang setelah dipotong ke
// from..to. Jika siaran sempat terputus (masih dalam batas tenggang),
// MediaMTX membuat beberapa rentang; hanya yang terpanjang yang disimpan,
// karena potongan fMP4 dari rentang berbeda tidak bisa disambung begitu
// saja menjadi satu file.
func longestSpan(spans []PlaybackSpan, from, to time.Time) (PlaybackSpan, bool) {
	var best PlaybackSpan
	for _, s := range spans {
		start, end := s.Start, s.End()
		if start.Before(from) {
			start = from
		}
		if end.After(to) {
			end = to
		}
		if d := end.Sub(start); d > best.Duration {
			best = PlaybackSpan{Start: start, Duration: d}
		}
	}
	return best, best.Duration > 0
}
