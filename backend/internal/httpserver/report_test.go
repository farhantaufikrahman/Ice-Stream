package httpserver

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

func (f *fakeVideoClient) ReportVideo(_ context.Context, in *videov1.ReportVideoRequest, _ ...grpc.CallOption) (*videov1.ReportVideoResponse, error) {
	f.lastReport = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.ReportVideoResponse{ReportedAt: timestamppb.New(time.Date(2026, 11, 11, 3, 0, 0, 0, time.UTC))}, nil
}

func TestReportVideo(t *testing.T) {
	verifier, as := newTokenSigner(t)
	fake := &fakeVideoClient{}
	r := newTestRouter(t, Deps{Videos: fake, Verifier: verifier})

	rec := do(r, http.MethodPost, "/api/v1/videos/42/reports", `{"reason":"spam"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "wajib login")

	rec = do(r, http.MethodPost, "/api/v1/videos/42/reports", `{"reason":"copyright","details":"lagu saya"}`, as(9))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"reported_at":"2026-11-11T03:00:00Z"}`, rec.Body.String())
	assert.Equal(t, int64(9), fake.lastReport.GetRequesterId(), "pelapor dari access token")
	assert.Equal(t, int64(42), fake.lastReport.GetVideoId())
	assert.Equal(t, videov1.ReportReason_REPORT_REASON_COPYRIGHT, fake.lastReport.GetReason())
	assert.Equal(t, "lagu saya", fake.lastReport.GetDetails())

	fake.lastReport = nil
	for _, body := range []string{`{"reason":"SPAM"}`, `{}`, `rusak`} {
		rec = do(r, http.MethodPost, "/api/v1/videos/42/reports", body, as(9))
		assert.Equal(t, http.StatusBadRequest, rec.Code, body)
	}
	assert.Nil(t, fake.lastReport, "alasan salah tidak sampai ke VideoService")

	fake.err = status.Error(codes.InvalidArgument, "video_id: tidak bisa melaporkan video sendiri")
	rec = do(r, http.MethodPost, "/api/v1/videos/42/reports", `{"reason":"spam"}`, as(7))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "video sendiri")

	fake.err = status.Error(codes.NotFound, "video tidak ditemukan")
	rec = do(r, http.MethodPost, "/api/v1/videos/999/reports", `{"reason":"spam"}`, as(9))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
