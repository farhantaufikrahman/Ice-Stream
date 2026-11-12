package livestream

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/farhantaufikrahman/ice-stream/backend/internal/video"
)

func TestPlaybackClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch r.URL.Path {
		case "/list":
			if q.Get("path") == "live/8" {
				http.Error(w, "no recordings found", http.StatusNotFound)
				return
			}
			assert.Equal(t, "live/7", q.Get("path"))
			assert.Equal(t, "2026-11-12T07:00:00Z", q.Get("start"))
			_, _ = io.WriteString(w, `[{"start":"2026-11-12T07:00:01.5Z","duration":64.25,"url":"http://x/get?..."}]`)
		case "/get":
			assert.Equal(t, "live/7", q.Get("path"))
			assert.Equal(t, "2026-11-12T07:00:01.5Z", q.Get("start"))
			assert.Equal(t, "64.250", q.Get("duration"))
			assert.Equal(t, "fmp4", q.Get("format"))
			_, _ = io.WriteString(w, "isi fmp4")
		}
	}))
	defer srv.Close()
	c := PlaybackClient{BaseURL: srv.URL}
	ctx := context.Background()
	start := time.Date(2026, 11, 12, 7, 0, 0, 0, time.UTC)

	spans, err := c.List(ctx, "live/7", start, start.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, spans, 1)
	assert.Equal(t, start.Add(1500*time.Millisecond), spans[0].Start)
	assert.Equal(t, 64250*time.Millisecond, spans[0].Duration)

	body, err := c.Get(ctx, "live/7", spans[0].Start, spans[0].Duration)
	require.NoError(t, err)
	b, _ := io.ReadAll(body)
	_ = body.Close()
	assert.Equal(t, "isi fmp4", string(b))

	spans, err = c.List(ctx, "live/8", start, start.Add(time.Hour))
	require.NoError(t, err)
	assert.Empty(t, spans, "404 = belum ada rekaman")
}

func TestLongestSpan(t *testing.T) {
	t0 := time.Date(2026, 11, 12, 7, 0, 0, 0, time.UTC)
	from, to := t0, t0.Add(10*time.Minute)
	got, ok := longestSpan([]PlaybackSpan{
		{Start: t0.Add(-time.Minute), Duration: 3 * time.Minute}, // dipotong jadi 2 menit
		{Start: t0.Add(4 * time.Minute), Duration: time.Hour},    // dipotong jadi 6 menit
	}, from, to)
	require.True(t, ok)
	assert.Equal(t, PlaybackSpan{Start: t0.Add(4 * time.Minute), Duration: 6 * time.Minute}, got)

	_, ok = longestSpan([]PlaybackSpan{{Start: t0.Add(-time.Hour), Duration: time.Minute}}, from, to)
	assert.False(t, ok, "rentang di luar siaran diabaikan")
}

type fakeRecSource struct {
	spans []PlaybackSpan
	got   []PlaybackSpan
}

func (f *fakeRecSource) List(context.Context, string, time.Time, time.Time) ([]PlaybackSpan, error) {
	return f.spans, nil
}

func (f *fakeRecSource) Get(_ context.Context, _ string, start time.Time, d time.Duration) (io.ReadCloser, error) {
	f.got = append(f.got, PlaybackSpan{Start: start, Duration: d})
	return io.NopCloser(strings.NewReader("rekaman")), nil
}

type fakeSink struct {
	imported    []video.ImportRecordingInput
	completeErr error
	completed   []int64
}

func (f *fakeSink) ImportRecording(_ context.Context, in video.ImportRecordingInput) (video.Video, error) {
	b, _ := io.ReadAll(in.Body)
	if len(b) == 0 {
		return video.Video{}, video.ErrRecordingEmpty
	}
	f.imported = append(f.imported, in)
	return video.Video{ID: 500 + int64(len(f.imported)), OwnerID: in.OwnerID}, nil
}

func (f *fakeSink) CompleteUpload(_ context.Context, _, videoID int64) (video.Video, error) {
	if f.completeErr != nil {
		return video.Video{}, f.completeErr
	}
	f.completed = append(f.completed, videoID)
	return video.Video{ID: videoID}, nil
}

func TestRecordingImporter(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepo()
	t0 := time.Date(2026, 11, 12, 7, 0, 0, 0, time.UTC)
	repo.streams = []Stream{
		{ID: 1, OwnerID: 7, Title: "Main bareng", Category: "game", Status: StatusEnded, StartedAt: t0, EndedAt: t0.Add(time.Hour)},
		{ID: 2, OwnerID: 8, Title: "Tidak direkam", Status: StatusEnded, StartedAt: t0, EndedAt: t0.Add(time.Hour)},
	}
	repo.recordings[1] = Recording{Status: RecordingPending}
	src := &fakeRecSource{spans: []PlaybackSpan{{Start: t0.Add(2 * time.Second), Duration: time.Hour}}}
	sink := &fakeSink{completeErr: errors.New("NATS mati")}
	now := t0.Add(2 * time.Hour)
	im := RecordingImporter{Repo: repo, Source: src, Sink: sink, Logger: slog.New(slog.DiscardHandler), Now: func() time.Time { return now }}

	// NATS mati: video sudah dibuat dan dicatat, status tetap IMPORTING.
	done, err := im.RunOnce(ctx)
	assert.True(t, done)
	assert.Error(t, err)
	require.Len(t, sink.imported, 1)
	in := sink.imported[0]
	assert.Equal(t, int64(7), in.OwnerID)
	assert.Equal(t, "Main bareng", in.Title)
	assert.Equal(t, "game", in.Category)
	assert.Equal(t, "Rekaman siaran langsung 12/11/2026 pukul 14.00 WIB.", in.Description)
	assert.Equal(t, int64(501), repo.streams[0].RecordingVideoID)
	assert.Equal(t, RecordingImporting, repo.recordings[1].Status)

	// Belum lewat 30 menit: tidak diambil lagi.
	done, err = im.RunOnce(ctx)
	require.NoError(t, err)
	assert.False(t, done)

	// Setelah 30 menit dan NATS pulih: lanjut tanpa mengimpor ulang.
	now = now.Add(31 * time.Minute)
	sink.completeErr = nil
	done, err = im.RunOnce(ctx)
	require.NoError(t, err)
	assert.True(t, done)
	assert.Len(t, sink.imported, 1, "tidak ada video dobel")
	assert.Equal(t, []int64{501}, sink.completed)
	rec, err := repo.LastRecording(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, RecordingDone, rec.Status)
	assert.Equal(t, int64(501), rec.VideoID)
	_, err = repo.LastRecording(ctx, 8)
	assert.ErrorIs(t, err, ErrNotFound, "siaran tanpa pilihan rekaman")
}

func TestRecordingImporterFailures(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepo()
	t0 := time.Date(2026, 11, 12, 7, 0, 0, 0, time.UTC)
	repo.streams = []Stream{
		{ID: 1, OwnerID: 7, Title: "a", Status: StatusEnded, StartedAt: t0, EndedAt: t0.Add(time.Hour)},
		{ID: 2, OwnerID: 7, Title: "b", Status: StatusEnded, StartedAt: t0.Add(2 * time.Hour), EndedAt: t0.Add(3 * time.Hour), RecordingVideoID: 9},
	}
	repo.recordings[1] = Recording{Status: RecordingPending}
	src := &fakeRecSource{} // tidak ada rekaman (misalnya sudah lewat 1 hari)
	sink := &fakeSink{}
	im := RecordingImporter{Repo: repo, Source: src, Sink: sink, Logger: slog.New(slog.DiscardHandler)}

	done, err := im.RunOnce(ctx)
	require.NoError(t, err)
	assert.True(t, done)
	assert.Equal(t, Recording{Status: RecordingFailed, Error: "rekaman tidak ditemukan (rekaman disimpan 1 hari)"}, repo.recordings[1])

	// Video rekaman sudah dihapus kreator sebelum dikirim ke transcoder.
	repo.recordings[2] = Recording{Status: RecordingPending}
	sink.completeErr = video.ErrNotFound
	done, err = im.RunOnce(ctx)
	require.NoError(t, err)
	assert.True(t, done)
	assert.Equal(t, "video rekaman sudah dihapus", repo.recordings[2].Error)
}
