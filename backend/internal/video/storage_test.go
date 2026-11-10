package video

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublicReadPolicy(t *testing.T) {
	policy, err := publicReadPolicy("videos", PublicPrefix)
	require.NoError(t, err)

	var doc struct {
		Statement []struct {
			Effect    string
			Principal map[string][]string
			Action    []string
			Resource  []string
		}
	}
	require.NoError(t, json.Unmarshal([]byte(policy), &doc))
	require.Len(t, doc.Statement, 1)
	st := doc.Statement[0]
	assert.Equal(t, "Allow", st.Effect)
	assert.Equal(t, []string{"*"}, st.Principal["AWS"])
	assert.Equal(t, []string{"s3:GetObject"}, st.Action, "hanya membaca, tidak bisa daftar isi/unggah/hapus")
	assert.Equal(t, []string{"arn:aws:s3:::videos/hls/*"}, st.Resource, "folder raw/ tetap privat")
}
