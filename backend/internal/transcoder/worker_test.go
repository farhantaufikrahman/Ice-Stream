package transcoder

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/farhantaufikrahman/ice-stream/backend/internal/events"
)

// fakeMsg menggantikan pesan NATS dan mencatat bagaimana pesan diakhiri.
type fakeMsg struct {
	data      []byte
	delivered uint64
	mu        sync.Mutex
	result    string // "ack", "nak", "term"
}

func newMsg(t *testing.T, v any, delivered uint64) *fakeMsg {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return &fakeMsg{data: data, delivered: delivered}
}

func (m *fakeMsg) set(r string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.result = r
	return nil
}

func (m *fakeMsg) Metadata() (*jetstream.MsgMetadata, error) {
	return &jetstream.MsgMetadata{NumDelivered: m.delivered}, nil
}
func (m *fakeMsg) Data() []byte                     { return m.data }
func (m *fakeMsg) Headers() nats.Header             { return nil }
func (m *fakeMsg) Subject() string                  { return events.SubjectUploaded }
func (m *fakeMsg) Reply() string                    { return "" }
func (m *fakeMsg) Ack() error                       { return m.set("ack") }
func (m *fakeMsg) DoubleAck(context.Context) error  { return m.set("ack") }
func (m *fakeMsg) Nak() error                       { return m.set("nak") }
func (m *fakeMsg) NakWithDelay(time.Duration) error { return m.set("nak") }
func (m *fakeMsg) InProgress() error                { return nil }
func (m *fakeMsg) Term() error                      { return m.set("term") }
func (m *fakeMsg) TermWithReason(string) error      { return m.set("term") }

// fakePublisher mencatat subject event yang dikirim.
type fakePublisher struct {
	subjects []string
	payloads []any
}

func (p *fakePublisher) Publish(_ context.Context, subject, _ string, v any) error {
	p.subjects = append(p.subjects, subject)
	p.payloads = append(p.payloads, v)
	return nil
}

// fakeStorage "mengunduh" file kosong dan mencatat file yang diunggah.
type fakeStorage struct {
	downloadErr error
	uploaded    []string
}

func (s *fakeStorage) Download(_ context.Context, _, dst string) error {
	if s.downloadErr != nil {
		return s.downloadErr
	}
	return os.WriteFile(dst, []byte("video"), 0o600)
}

func (s *fakeStorage) UploadDir(_ context.Context, dir, prefix string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		s.uploaded = append(s.uploaded, prefix+filepath.ToSlash(rel))
		return nil
	})
}

// fakeMedia pura-pura menjadi ffmpeg: membuat file hasil kosong.
type fakeMedia struct {
	probeErr     error
	transcodeErr error
}

func (m *fakeMedia) Probe(context.Context, string) (ProbeResult, error) {
	return ProbeResult{Width: 1280, Height: 720, DurationMs: 9000, HasAudio: true}, m.probeErr
}

func (m *fakeMedia) TranscodeHLS(_ context.Context, _, outDir string, renditions []Rendition, _ bool) error {
	if m.transcodeErr != nil {
		return m.transcodeErr
	}
	for _, r := range renditions {
		if err := os.MkdirAll(filepath.Join(outDir, r.Name), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(outDir, r.Name, "index.m3u8"), []byte("#EXTM3U"), 0o600); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(outDir, "master.m3u8"), []byte("#EXTM3U"), 0o600)
}

func (m *fakeMedia) Frame(_ context.Context, _, output string, _ int64) error {
	return os.WriteFile(output, []byte("jpg"), 0o600)
}

func newTestWorker(t *testing.T, media *fakeMedia, storage *fakeStorage) (*Worker, *fakePublisher) {
	pub := &fakePublisher{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewWorker(media, storage, pub, t.TempDir(), time.Minute, logger), pub
}

var uploaded = events.Uploaded{VideoID: 42, ObjectKey: "raw/abc/source", ContentType: "video/mp4"}

func TestHandleSuccess(t *testing.T) {
	storage := &fakeStorage{}
	w, pub := newTestWorker(t, &fakeMedia{}, storage)
	msg := newMsg(t, uploaded, 1)

	w.Handle(context.Background(), msg)

	assert.Equal(t, "ack", msg.result)
	assert.Equal(t, []string{events.SubjectTranscodeStarted, events.SubjectTranscodeSucceeded}, pub.subjects)
	res := pub.payloads[1].(events.TranscodeSucceeded)
	// Folder hasil: hls/42/<32 huruf hex acak>/ (langkah 26).
	require.Regexp(t, `^hls/42/[0-9a-f]{32}/master\.m3u8$`, res.PlaylistKey)
	prefix := strings.TrimSuffix(res.PlaylistKey, "master.m3u8")
	// Durasi palsu 9 detik: bingkai di detik 3 (bawaan), 3,6 (40%), dan 6,75 (75%).
	assert.Equal(t, []int64{3000, 3600, 6750}, res.FrameTimesMs)
	assert.Equal(t, prefix+"frames/3000.jpg", res.ThumbnailKey)
	assert.Equal(t, int64(9000), res.DurationMs)
	assert.Equal(t, []string{"360p", "720p"}, res.Renditions)
	assert.ElementsMatch(t, []string{
		prefix + "master.m3u8", prefix + "360p/index.m3u8", prefix + "720p/index.m3u8",
		prefix + "frames/3000.jpg", prefix + "frames/3600.jpg", prefix + "frames/6750.jpg",
	}, storage.uploaded)

	// Percobaan berikutnya memakai folder acak yang lain.
	w.Handle(context.Background(), newMsg(t, uploaded, 2))
	again := pub.payloads[3].(events.TranscodeSucceeded)
	assert.NotEqual(t, res.PlaylistKey, again.PlaylistKey)
}

func TestFramePositions(t *testing.T) {
	assert.Equal(t, []int64{3000, 300000, 562500}, FramePositions(750000))
	// Video 6 detik: bingkai bawaan di sepertiga durasi.
	assert.Equal(t, []int64{2000, 2400, 4500}, FramePositions(6000))
	// Video yang sangat pendek: posisi yang sama dibuang.
	assert.Equal(t, []int64{0}, FramePositions(1))
	assert.Equal(t, []int64{0}, FramePositions(0))
}

func TestHandleTemporaryErrorRetries(t *testing.T) {
	w, pub := newTestWorker(t, &fakeMedia{transcodeErr: errors.New("ffmpeg kehabisan memori")}, &fakeStorage{})
	msg := newMsg(t, uploaded, 1)

	w.Handle(context.Background(), msg)

	assert.Equal(t, "nak", msg.result, "dicoba lagi nanti")
	assert.Equal(t, []string{events.SubjectTranscodeStarted}, pub.subjects)
}

func TestHandleLastAttemptFails(t *testing.T) {
	w, pub := newTestWorker(t, &fakeMedia{transcodeErr: errors.New("ffmpeg kehabisan memori")}, &fakeStorage{})
	msg := newMsg(t, uploaded, MaxAttempts)

	w.Handle(context.Background(), msg)

	assert.Equal(t, "term", msg.result)
	assert.Equal(t, []string{events.SubjectTranscodeStarted, events.SubjectTranscodeFailed}, pub.subjects)
	assert.Equal(t, "transcoding gagal setelah 3 kali percobaan", pub.payloads[1].(events.TranscodeFailed).Reason,
		"detail teknis hanya di log, bukan untuk user")
}

func TestHandlePermanentErrors(t *testing.T) {
	tests := map[string]struct {
		media   *fakeMedia
		storage *fakeStorage
	}{
		"bukan file video":      {&fakeMedia{probeErr: ErrNoVideoStream}, &fakeStorage{}},
		"file mentah tidak ada": {&fakeMedia{}, &fakeStorage{downloadErr: ErrSourceMissing}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			w, pub := newTestWorker(t, tt.media, tt.storage)
			msg := newMsg(t, uploaded, 1)

			w.Handle(context.Background(), msg)

			assert.Equal(t, "term", msg.result, "langsung FAILED tanpa mencoba lagi")
			last := len(pub.subjects) - 1
			assert.Equal(t, events.SubjectTranscodeFailed, pub.subjects[last])
			assert.NotContains(t, pub.payloads[last].(events.TranscodeFailed).Reason, "/", "tanpa path file internal")
		})
	}
}

func TestHandleShutdownReturnsJob(t *testing.T) {
	w, pub := newTestWorker(t, &fakeMedia{transcodeErr: context.Canceled}, &fakeStorage{})
	msg := newMsg(t, uploaded, MaxAttempts)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	w.Handle(ctx, msg)

	assert.Equal(t, "nak", msg.result, "dikembalikan, bukan FAILED")
	assert.NotContains(t, pub.subjects, events.SubjectTranscodeFailed)
}

func TestHandleInvalidPayload(t *testing.T) {
	w, pub := newTestWorker(t, &fakeMedia{}, &fakeStorage{})
	msg := &fakeMsg{data: []byte("bukan json"), delivered: 1}

	w.Handle(context.Background(), msg)

	assert.Equal(t, "term", msg.result)
	assert.Empty(t, pub.subjects)
}
