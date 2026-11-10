package transcoder

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func names(rs []Rendition) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Name
	}
	return out
}

func TestRenditions(t *testing.T) {
	assert.Equal(t, []string{"360p", "720p"}, names(Renditions(1080)))
	assert.Equal(t, []string{"360p", "720p"}, names(Renditions(720)))
	assert.Equal(t, []string{"360p"}, names(Renditions(480)), "tidak diperbesar ke 720p")
	assert.Equal(t, []string{"240p"}, names(Renditions(240)), "video kecil memakai tinggi aslinya")
	assert.Equal(t, []string{"122p"}, names(Renditions(123)), "dibulatkan ke angka genap")
}

func TestParseProbe(t *testing.T) {
	res, err := parseProbe([]byte(`{
		"streams": [
			{"codec_type": "video", "width": 1920, "height": 1080},
			{"codec_type": "audio"}
		],
		"format": {"duration": "12.3456"}
	}`))
	require.NoError(t, err)
	assert.Equal(t, ProbeResult{Width: 1920, Height: 1080, DurationMs: 12346, HasAudio: true}, res)

	// Video HP yang direkam tegak: disimpan 1920x1080 dengan rotasi -90.
	res, err = parseProbe([]byte(`{
		"streams": [{"codec_type": "video", "width": 1920, "height": 1080, "side_data_list": [{"rotation": -90}]}],
		"format": {"duration": "5"}
	}`))
	require.NoError(t, err)
	assert.Equal(t, 1080, res.Width)
	assert.Equal(t, 1920, res.Height)
	assert.False(t, res.HasAudio)

	_, err = parseProbe([]byte(`{"streams": [{"codec_type": "audio"}], "format": {"duration": "5"}}`))
	assert.ErrorIs(t, err, ErrNoVideoStream)

	_, err = parseProbe([]byte(`{"streams": [{"codec_type": "video", "width": 2, "height": 2}], "format": {}}`))
	assert.Error(t, err, "durasi kosong")
}

func TestHLSArgs(t *testing.T) {
	f := FFmpeg{Preset: "veryfast", Threads: 2}
	args := strings.Join(f.HLSArgs("in.mp4", "/out", Renditions(720), true), " ")
	assert.Contains(t, args, "[0:v]split=2[v0][v1];[v0]scale=-2:360[v0out];[v1]scale=-2:720[v1out]")
	assert.Contains(t, args, "-var_stream_map v:0,a:0,name:360p v:1,a:1,name:720p")
	assert.Contains(t, args, "-maxrate:v:1 2800k")
	assert.Contains(t, args, "-c:a aac")

	noAudio := strings.Join(f.HLSArgs("in.mp4", "/out", Renditions(480), false), " ")
	assert.Contains(t, noAudio, "-var_stream_map v:0,name:360p")
	assert.NotContains(t, noAudio, "0:a:0")
	assert.NotContains(t, noAudio, "-c:a")
}

// TestFFmpegIntegration menjalankan ffmpeg sungguhan. Dilewati jika ffmpeg
// tidak terpasang (misalnya di laptop tanpa ffmpeg).
func TestFFmpegIntegration(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg tidak terpasang")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe tidak terpasang")
	}
	ctx := context.Background()
	dir := t.TempDir()
	src := filepath.Join(dir, "source")

	// Buat video uji 8 detik 1280x720 dengan suara, lalu simpan TANPA
	// ekstensi (seperti file mentah di storage).
	gen := exec.Command("ffmpeg", "-nostdin", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=30:duration=8",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=8",
		"-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", "-shortest", "-f", "mp4", src)
	out, err := gen.CombinedOutput()
	require.NoError(t, err, string(out))

	f := FFmpeg{Preset: "ultrafast", Threads: 2}
	probe, err := f.Probe(ctx, src)
	require.NoError(t, err)
	assert.Equal(t, 720, probe.Height)
	assert.True(t, probe.HasAudio)
	assert.InDelta(t, 8000, probe.DurationMs, 100)

	hls := filepath.Join(dir, "hls")
	require.NoError(t, f.TranscodeHLS(ctx, src, hls, Renditions(probe.Height), probe.HasAudio))
	require.NoError(t, f.Thumbnail(ctx, src, filepath.Join(hls, "thumbnail.jpg"), probe.DurationMs))

	master, err := os.ReadFile(filepath.Join(hls, "master.m3u8"))
	require.NoError(t, err)
	assert.Contains(t, string(master), "360p/index.m3u8")
	assert.Contains(t, string(master), "720p/index.m3u8")
	assert.Contains(t, string(master), "RESOLUTION=1280x720")

	for _, name := range []string{"360p/index.m3u8", "720p/index.m3u8", "360p/segment_000.ts", "720p/segment_001.ts", "thumbnail.jpg"} {
		info, err := os.Stat(filepath.Join(hls, name))
		require.NoError(t, err, name)
		assert.Positive(t, info.Size(), name)
	}
	playlist, err := os.ReadFile(filepath.Join(hls, "720p/index.m3u8"))
	require.NoError(t, err)
	assert.Contains(t, string(playlist), "#EXT-X-PLAYLIST-TYPE:VOD")
	assert.Contains(t, string(playlist), "#EXT-X-ENDLIST")

	// File yang bukan video ditolak ffprobe.
	bad := filepath.Join(dir, "bukan-video")
	require.NoError(t, os.WriteFile(bad, []byte("halo, ini teks biasa"), 0o600))
	_, err = f.Probe(ctx, bad)
	assert.Error(t, err)
}
