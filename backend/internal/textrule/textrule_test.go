package textrule

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSingleLine(t *testing.T) {
	got, err := SingleLine("  halo 👋  ", 100)
	require.NoError(t, err)
	assert.Equal(t, "halo 👋", got)

	got, err = SingleLine("👩‍💻", 100)
	require.NoError(t, err, "zero width joiner untuk emoji tetap boleh")
	assert.Equal(t, "👩‍💻", got)

	_, err = SingleLine(strings.Repeat("é", 100), 100)
	assert.NoError(t, err, "100 huruf multibyte masih boleh")

	for name, in := range map[string]string{
		"kosong":        "   ",
		"kepanjangan":   strings.Repeat("a", 101),
		"baris baru":    "a\nb",
		"tab":           "a\tb",
		"kontrol":       "a\x07b",
		"pembalik arah": "a‮b",
		"bukan utf8":    string([]byte{0xff}),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := SingleLine(in, 100)
			assert.Error(t, err)
		})
	}
}
