package video

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

func TestPartPlan(t *testing.T) {
	const mb = int64(1 << 20)
	assert.Equal(t, int32(1), partPlan(1, 50*mb))
	assert.Equal(t, int32(1), partPlan(50*mb, 50*mb))
	assert.Equal(t, int32(2), partPlan(50*mb+1, 50*mb))
	assert.Equal(t, int32(41), partPlan(2048*mb, 50*mb), "video 2 GB")
	assert.Equal(t, 50*mb, partSizeOf(1, 120*mb, 50*mb))
	assert.Equal(t, 20*mb, partSizeOf(3, 120*mb, 50*mb), "bagian terakhir berisi sisa")
}

// newMultipartService: bagian 500 byte, jadi video 1234 byte = 3 bagian
// (500, 500, 234).
func newMultipartService(t *testing.T) (*Service, *fakeStore, Upload) {
	t.Helper()
	svc, _, store, _ := newTestServiceWithEvents()
	svc.opts.UploadPartBytes = 500
	up, err := svc.CreateUpload(context.Background(), validInput())
	require.NoError(t, err)
	require.Equal(t, int32(3), up.PartCount)
	return svc, store, up
}

func TestPresignUploadParts(t *testing.T) {
	ctx := context.Background()
	svc, store, up := newMultipartService(t)
	id := up.Video.ID

	out, err := svc.PresignUploadParts(ctx, 7, id, []int32{3, 1, 3})
	require.NoError(t, err)
	require.Len(t, out.Parts, 2, "nomor dobel diminta sekali")
	assert.Equal(t, int32(1), out.Parts[0].Number)
	assert.Equal(t, int64(500), out.Parts[0].Size)
	assert.Equal(t, int32(3), out.Parts[1].Number)
	assert.Equal(t, int64(234), out.Parts[1].Size)
	assert.Contains(t, out.Parts[1].Request.URL, "partNumber=3&size=234")
	assert.Empty(t, out.Uploaded)
	assert.WithinDuration(t, time.Now().Add(15*time.Minute), out.ExpiresAt, time.Minute)

	// Melanjutkan upload: bagian yang sudah tersimpan dilaporkan; bagian
	// dengan ukuran salah dianggap belum terkirim.
	store.putPart(up.Video.UploadID, 1, 500)
	store.putPart(up.Video.UploadID, 2, 499)
	out, err = svc.PresignUploadParts(ctx, 7, id, nil)
	require.NoError(t, err)
	assert.Empty(t, out.Parts)
	assert.Equal(t, []int32{1}, out.Uploaded)

	_, err = svc.PresignUploadParts(ctx, 8, id, []int32{1})
	assert.ErrorIs(t, err, ErrNotFound, "bukan pemilik")

	for name, numbers := range map[string][]int32{
		"nomor nol":       {0},
		"melebihi jumlah": {4},
		"terlalu banyak":  make([]int32, 21),
	} {
		_, err = svc.PresignUploadParts(ctx, 7, id, numbers)
		var verr *ValidationError
		assert.ErrorAs(t, err, &verr, name)
	}
}

func TestCompleteMultipartUpload(t *testing.T) {
	ctx := context.Background()
	svc, store, up := newMultipartService(t)
	id, uploadID := up.Video.ID, up.Video.UploadID

	// Baru 2 dari 3 bagian.
	store.putPart(uploadID, 1, 500)
	store.putPart(uploadID, 3, 234)
	_, err := svc.CompleteUpload(ctx, 7, id)
	assert.ErrorIs(t, err, ErrUploadIncomplete)

	// Semua bagian ada: storage menggabungkannya, status UPLOADED.
	store.putPart(uploadID, 2, 500)
	v, err := svc.CompleteUpload(ctx, 7, id)
	require.NoError(t, err)
	assert.Equal(t, StatusUploaded, v.Status)
	assert.Equal(t, ObjectInfo{Size: 1234, ContentType: "video/mp4"}, store.objects[up.Video.ObjectKey])

	// URL bagian tidak diberikan lagi setelah upload selesai.
	_, err = svc.PresignUploadParts(ctx, 7, id, []int32{1})
	assert.ErrorIs(t, err, ErrNotUploading)
}

func TestPurgeAbortsPendingMultipart(t *testing.T) {
	ctx := context.Background()
	svc, store, up := newMultipartService(t)
	store.putPart(up.Video.UploadID, 1, 500)

	v, err := svc.DeleteVideo(ctx, 7, up.Video.ID)
	require.NoError(t, err)
	_, err = svc.PurgeMedia(ctx, v)
	require.NoError(t, err)
	assert.Equal(t, []string{up.Video.UploadID}, store.aborted)
}

func TestGRPCPresignUploadParts(t *testing.T) {
	ctx := context.Background()
	svc, store, up := newMultipartService(t)
	srv := NewGRPCServer(svc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	store.putPart(up.Video.UploadID, 1, 500)

	resp, err := srv.PresignUploadParts(ctx, &videov1.PresignUploadPartsRequest{
		RequesterId: 7, VideoId: up.Video.ID, PartNumbers: []int32{2, 3},
	})
	require.NoError(t, err)
	require.Len(t, resp.GetParts(), 2)
	assert.Equal(t, int32(2), resp.GetParts()[0].GetPartNumber())
	assert.Equal(t, int64(234), resp.GetParts()[1].GetSizeBytes())
	assert.NotEmpty(t, resp.GetParts()[0].GetUrl())
	assert.Equal(t, []int32{1}, resp.GetUploadedPartNumbers())
	assert.True(t, resp.GetExpiresAt().AsTime().After(time.Now()))

	// Upload multipart sudah hilang di storage: FailedPrecondition, supaya
	// gateway menjawab 409 dan frontend tahu harus mulai dari awal.
	require.NoError(t, store.AbortMultipart(ctx, "", up.Video.UploadID))
	_, err = srv.PresignUploadParts(ctx, &videov1.PresignUploadPartsRequest{RequesterId: 7, VideoId: up.Video.ID})
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))

	_, err = srv.PresignUploadParts(ctx, &videov1.PresignUploadPartsRequest{RequesterId: 8, VideoId: up.Video.ID})
	assert.Equal(t, codes.NotFound, status.Code(err), "bukan pemilik")
}
