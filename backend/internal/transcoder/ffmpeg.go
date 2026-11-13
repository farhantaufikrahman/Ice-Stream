// Package transcoder mengubah video mentah hasil upload menjadi HLS
// (HTTP Live Streaming) memakai ffmpeg.
//
// HLS memecah video menjadi potongan pendek (.ts, sekitar 6 detik) dan
// daftar putar (.m3u8). Player bisa mulai memutar sebelum seluruh file
// terunduh, dan bisa pindah resolusi (360p/720p) sesuai kecepatan internet.
// Cara ini dipakai YouTube, Bilibili, dan Netflix.
package transcoder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
)

// segmentSeconds adalah panjang satu potongan HLS. 6 detik adalah anjuran
// Apple untuk HLS.
const segmentSeconds = 6

// Rendition adalah satu pilihan resolusi, misalnya 720p.
type Rendition struct {
	Name   string // nama folder dan label, misalnya "720p"
	Height int    // tinggi gambar dalam piksel
	// MaxBitrateK dan BufSizeK (kbit/s) membatasi bitrate supaya ukuran
	// potongan stabil untuk streaming.
	MaxBitrateK int
	BufSizeK    int
}

// ladder adalah daftar resolusi yang mungkin dibuat, dari kecil ke besar.
// Sengaja hanya dua supaya ringan untuk PC lokal.
var ladder = []Rendition{
	{Name: "360p", Height: 360, MaxBitrateK: 800, BufSizeK: 1600},
	{Name: "720p", Height: 720, MaxBitrateK: 2800, BufSizeK: 5600},
}

// Renditions memilih resolusi untuk video setinggi sourceHeight piksel.
// Video tidak pernah diperbesar: video 480p hanya dibuat 360p, dan video
// yang lebih kecil dari 360p dibuat dengan tinggi aslinya.
func Renditions(sourceHeight int) []Rendition {
	var out []Rendition
	for _, r := range ladder {
		if r.Height <= sourceHeight {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		h := sourceHeight &^ 1 // H.264 butuh ukuran genap
		if h < 2 {
			h = 2
		}
		low := ladder[0]
		out = append(out, Rendition{Name: strconv.Itoa(h) + "p", Height: h, MaxBitrateK: low.MaxBitrateK, BufSizeK: low.BufSizeK})
	}
	return out
}

// ProbeResult adalah informasi file video dari ffprobe.
type ProbeResult struct {
	// Width dan Height adalah ukuran gambar yang tampil di layar (rotasi
	// video dari HP sudah diperhitungkan).
	Width, Height int
	DurationMs    int64
	HasAudio      bool
}

// ErrNoVideoStream berarti file tidak berisi gambar video sama sekali
// (misalnya file audio atau file rusak).
var ErrNoVideoStream = errors.New("file tidak berisi stream video")

// FFmpeg menjalankan program ffmpeg dan ffprobe.
type FFmpeg struct {
	// Preset dan Threads, lihat config.TranscoderConfig.
	Preset  string
	Threads int
}

// Probe membaca ukuran, durasi, dan ada tidaknya suara dari file video.
func (f FFmpeg) Probe(ctx context.Context, path string) (ProbeResult, error) {
	out, err := run(ctx, "ffprobe",
		"-v", "error",
		"-print_format", "json",
		"-show_format", "-show_streams",
		path,
	)
	if err != nil {
		return ProbeResult{}, err
	}
	return parseProbe(out)
}

// probeOutput adalah bagian JSON ffprobe yang kita pakai.
type probeOutput struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
		// Video dari HP sering disimpan miring dengan catatan "putar 90°".
		SideDataList []struct {
			Rotation float64 `json:"rotation"`
		} `json:"side_data_list"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func parseProbe(data []byte) (ProbeResult, error) {
	var p probeOutput
	if err := json.Unmarshal(data, &p); err != nil {
		return ProbeResult{}, fmt.Errorf("membaca hasil ffprobe: %w", err)
	}
	var res ProbeResult
	foundVideo := false
	for _, s := range p.Streams {
		switch s.CodecType {
		case "video":
			if foundVideo {
				continue // pakai stream video pertama saja
			}
			foundVideo = true
			res.Width, res.Height = s.Width, s.Height
			for _, sd := range s.SideDataList {
				if r := int(math.Abs(sd.Rotation)); r == 90 || r == 270 {
					res.Width, res.Height = res.Height, res.Width
				}
			}
		case "audio":
			res.HasAudio = true
		}
	}
	if !foundVideo || res.Width <= 0 || res.Height <= 0 {
		return ProbeResult{}, ErrNoVideoStream
	}
	seconds, err := strconv.ParseFloat(p.Format.Duration, 64)
	if err != nil || seconds <= 0 {
		return ProbeResult{}, fmt.Errorf("durasi video tidak terbaca: %q", p.Format.Duration)
	}
	res.DurationMs = int64(math.Round(seconds * 1000))
	return res, nil
}

// HLSArgs menyusun argumen ffmpeg untuk membuat semua resolusi sekaligus
// dalam satu kali baca file (lebih hemat daripada menjalankan ffmpeg sekali
// per resolusi). Hasilnya:
//
//	outDir/master.m3u8          daftar resolusi (dibuka player)
//	outDir/360p/index.m3u8      daftar potongan 360p
//	outDir/360p/segment_000.ts  potongan video 6 detik
//	outDir/720p/...
func (f FFmpeg) HLSArgs(input, outDir string, renditions []Rendition, hasAudio bool) []string {
	n := len(renditions)

	// filter_complex: gandakan gambar menjadi n salinan, lalu perkecil
	// masing-masing. scale=-2:720 artinya tinggi 720, lebar menyesuaikan
	// (dibulatkan ke angka genap).
	var filter strings.Builder
	fmt.Fprintf(&filter, "[0:v]split=%d", n)
	for i := range renditions {
		fmt.Fprintf(&filter, "[v%d]", i)
	}
	for i, r := range renditions {
		fmt.Fprintf(&filter, ";[v%d]scale=-2:%d[v%dout]", i, r.Height, i)
	}

	args := []string{
		"-nostdin", "-hide_banner", "-loglevel", "error", "-y",
		"-i", input,
		"-filter_complex", filter.String(),
	}
	var streamMap []string
	for i, r := range renditions {
		args = append(args, "-map", fmt.Sprintf("[v%dout]", i))
		entry := fmt.Sprintf("v:%d", i)
		if hasAudio {
			args = append(args, "-map", "0:a:0")
			entry += fmt.Sprintf(",a:%d", i)
		}
		streamMap = append(streamMap, entry+",name:"+r.Name)
		args = append(args,
			fmt.Sprintf("-maxrate:v:%d", i), fmt.Sprintf("%dk", r.MaxBitrateK),
			fmt.Sprintf("-bufsize:v:%d", i), fmt.Sprintf("%dk", r.BufSizeK),
		)
	}
	args = append(args,
		// H.264 + yuv420p: diputar oleh semua browser dan HP.
		"-c:v", "libx264", "-preset", f.Preset, "-profile:v", "main", "-pix_fmt", "yuv420p",
		// CRF 23 = kualitas default x264; maxrate di atas menjadi batas atasnya.
		"-crf", "23",
		// Keyframe tepat setiap 6 detik, supaya setiap potongan bisa diputar
		// sendiri dan semua resolusi terpotong di titik yang sama.
		"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", segmentSeconds),
		"-sc_threshold", "0",
		"-threads", strconv.Itoa(f.Threads),
	)
	if hasAudio {
		args = append(args, "-c:a", "aac", "-b:a", "128k", "-ac", "2", "-ar", "48000")
	}
	args = append(args,
		"-f", "hls",
		"-hls_time", strconv.Itoa(segmentSeconds),
		"-hls_playlist_type", "vod",
		"-hls_flags", "independent_segments",
		"-hls_segment_filename", outDir+"/%v/segment_%03d.ts",
		"-master_pl_name", "master.m3u8",
		"-var_stream_map", strings.Join(streamMap, " "),
		outDir+"/%v/index.m3u8",
	)
	return args
}

// TranscodeHLS menjalankan ffmpeg dengan HLSArgs.
func (f FFmpeg) TranscodeHLS(ctx context.Context, input, outDir string, renditions []Rendition, hasAudio bool) error {
	_, err := run(ctx, "ffmpeg", f.HLSArgs(input, outDir, renditions, hasAudio)...)
	return err
}

// ThumbnailArgs menyusun argumen ffmpeg untuk mengambil satu gambar JPEG
// setinggi 360 piksel dari detik ke-atSeconds.
func (f FFmpeg) ThumbnailArgs(input, output string, atSeconds float64) []string {
	return []string{
		"-nostdin", "-hide_banner", "-loglevel", "error", "-y",
		// -ss sebelum -i: lompat langsung ke detik itu (cepat).
		"-ss", strconv.FormatFloat(atSeconds, 'f', 3, 64),
		"-i", input,
		"-frames:v", "1",
		"-vf", "scale=-2:360",
		"-q:v", "3", // kualitas JPEG (2 = terbaik, 31 = terburuk)
		"-threads", strconv.Itoa(f.Threads),
		output,
	}
}

// Frame membuat satu gambar dari detik ke-(atMs/1000).
func (f FFmpeg) Frame(ctx context.Context, input, output string, atMs int64) error {
	_, err := run(ctx, "ffmpeg", f.ThumbnailArgs(input, output, float64(atMs)/1000)...)
	return err
}

// FramePositions memilih posisi (milidetik) bingkai pilihan thumbnail
// (langkah 26, desain Upload menampilkan tiga bingkai):
//   - bingkai pertama = thumbnail bawaan, di detik ke-3, atau di sepertiga
//     durasi untuk video yang lebih pendek dari 9 detik (sama dengan
//     sebelum langkah 26);
//   - lalu di 40% dan 75% durasi, supaya kreator punya pilihan dari bagian
//     tengah dan akhir video.
//
// Posisi yang sama dibuang, jadi video yang sangat pendek bisa punya
// kurang dari tiga bingkai.
func FramePositions(durationMs int64) []int64 {
	if durationMs <= 0 {
		return []int64{0}
	}
	first := int64(math.Min(3000, float64(durationMs)/3))
	positions := []int64{first}
	for _, pct := range []int64{40, 75} {
		at := durationMs * pct / 100
		if at > positions[len(positions)-1] {
			positions = append(positions, at)
		}
	}
	return positions
}

// run menjalankan program dan mengembalikan stdout-nya. Jika gagal, pesan
// error dari stderr ikut dikembalikan (dipotong supaya tidak terlalu panjang).
func run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("%s dihentikan: %w", name, ctxErr)
		}
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 300 {
			msg = "..." + msg[len(msg)-300:]
		}
		return nil, fmt.Errorf("%s gagal (%w): %s", name, err, msg)
	}
	return stdout.Bytes(), nil
}
