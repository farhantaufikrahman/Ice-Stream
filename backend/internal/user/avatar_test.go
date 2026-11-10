package user

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makePNG membuat PNG w x h: setengah kiri merah, setengah kanan biru.
func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.NRGBA{R: 255, A: 255}
			if x >= w/2 {
				c = color.NRGBA{B: 255, A: 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func TestProcessAvatar(t *testing.T) {
	// Gambar landscape 600x300: dipotong persegi di tengah (x 150..450),
	// jadi hasilnya kiri merah dan kanan biru.
	out, err := ProcessAvatar(makePNG(t, 600, 300))
	require.NoError(t, err)

	img, err := jpeg.Decode(bytes.NewReader(out))
	require.NoError(t, err, "hasilnya selalu JPEG")
	assert.Equal(t, image.Rect(0, 0, AvatarSize, AvatarSize), img.Bounds())
	r, _, b, _ := img.At(10, AvatarSize/2).RGBA()
	assert.Greater(t, r, b, "sisi kiri merah")
	r, _, b, _ = img.At(AvatarSize-10, AvatarSize/2).RGBA()
	assert.Greater(t, b, r, "sisi kanan biru")
}

func TestProcessAvatarTransparentBecomesWhite(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 100, 100)))) // semua transparan
	out, err := ProcessAvatar(buf.Bytes())
	require.NoError(t, err)
	img, err := jpeg.Decode(bytes.NewReader(out))
	require.NoError(t, err)
	r, g, b, _ := img.At(50, 50).RGBA()
	assert.True(t, r > 0xf000 && g > 0xf000 && b > 0xf000, "latar putih, bukan hitam")
}

func TestProcessAvatarRejects(t *testing.T) {
	tests := map[string][]byte{
		"kosong":             nil,
		"bukan gambar":       []byte("<html><script>alert(1)</script></html>"),
		"terlalu kecil":      makePNG(t, 32, 32),
		"lebih dari 2 MiB":   append(makePNG(t, 100, 100), make([]byte, MaxAvatarBytes)...),
		"PNG rusak":          makePNG(t, 100, 100)[:60],
		"gambar raksasa":     hugePNGHeader(t),
		"GIF tidak diterima": []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ProcessAvatar(data)
			var verr *ValidationError
			assert.ErrorAs(t, err, &verr)
		})
	}
}

// hugePNGHeader membuat PNG 6000x6000 (36 megapiksel) yang filenya kecil
// karena warnanya polos. Harus ditolak sebelum isinya dibuka.
func hugePNGHeader(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	require.NoError(t, enc.Encode(&buf, image.NewGray(image.Rect(0, 0, 6000, 6000))))
	require.Less(t, buf.Len(), MaxAvatarBytes, "file kecil walau gambarnya raksasa")
	return buf.Bytes()
}

func TestCenterSquare(t *testing.T) {
	assert.Equal(t, image.Rect(200, 0, 1000, 800), centerSquare(image.Rect(0, 0, 1200, 800)))
	assert.Equal(t, image.Rect(0, 100, 400, 500), centerSquare(image.Rect(0, 0, 400, 600)))
}
