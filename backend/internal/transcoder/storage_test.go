package transcoder

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContentTypeAndCache(t *testing.T) {
	tests := []struct{ path, contentType, cache string }{
		{"hls/master.m3u8", "application/vnd.apple.mpegurl", "public, max-age=300"},
		{"hls/720p/segment_000.ts", "video/mp2t", "public, max-age=31536000, immutable"},
		{"hls/thumbnail.jpg", "image/jpeg", "public, max-age=31536000, immutable"},
		{"hls/lain.bin", "application/octet-stream", "public, max-age=31536000, immutable"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.contentType, contentType(tt.path), tt.path)
		assert.Equal(t, tt.cache, cacheControl(tt.path), tt.path)
	}
}
