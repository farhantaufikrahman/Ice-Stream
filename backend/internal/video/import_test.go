package video

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImportRecording(t *testing.T) {
	ctx := context.Background()
	svc, repo, store, pub := newTestServiceWithEvents()

	v, err := svc.ImportRecording(ctx, ImportRecordingInput{
		OwnerID: 7, Title: "  Main bareng  ", Description: "Rekaman siaran langsung", Category: "game",
		Body: strings.NewReader("isi rekaman"),
	})
	require.NoError(t, err)
	assert.Equal(t, StatusUploaded, v.Status)
	assert.True(t, v.Draft, "rekaman selalu jadi draf dulu")
	assert.Equal(t, VisibilityPublic, v.Visibility)
	assert.Equal(t, "Main bareng", v.Title)
	assert.Equal(t, int64(len("isi rekaman")), v.SizeBytes)
	assert.Equal(t, ObjectInfo{Size: v.SizeBytes, ContentType: "video/mp4"}, store.objects[v.ObjectKey])
	assert.NotEmpty(t, repo.terms[v.ID], "judul rekaman bisa dicari setelah terbit")
	assert.Empty(t, pub.published, "event dikirim pemanggil lewat CompleteUpload")

	// CompleteUpload mengirim event, dan aman diulang.
	_, err = svc.CompleteUpload(ctx, 7, v.ID)
	require.NoError(t, err)
	_, err = svc.CompleteUpload(ctx, 7, v.ID)
	require.NoError(t, err)
	assert.Len(t, pub.published, 2)
}

func TestImportRecordingRejects(t *testing.T) {
	ctx := context.Background()
	svc, repo, store := newTestService()

	_, err := svc.ImportRecording(ctx, ImportRecordingInput{OwnerID: 7, Title: "x", Body: strings.NewReader("")})
	assert.ErrorIs(t, err, ErrRecordingEmpty)

	// Batas unggah di test 10 MB.
	big := strings.NewReader(strings.Repeat("a", 10<<20+1))
	_, err = svc.ImportRecording(ctx, ImportRecordingInput{OwnerID: 7, Title: "x", Body: big})
	assert.ErrorContains(t, err, "lebih besar dari batas")

	var verr *ValidationError
	_, err = svc.ImportRecording(ctx, ImportRecordingInput{OwnerID: 7, Title: " ", Body: strings.NewReader("a")})
	assert.ErrorAs(t, err, &verr)

	store.putErr = errors.New("storage mati")
	_, err = svc.ImportRecording(ctx, ImportRecordingInput{OwnerID: 7, Title: "x", Body: strings.NewReader("a")})
	assert.ErrorContains(t, err, "storage mati")

	assert.Empty(t, repo.videos, "tidak ada video yang dibuat")
	assert.Empty(t, store.objects, "file yang ditolak dihapus lagi")
}
