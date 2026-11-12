package httpserver

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	moderationv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/moderation/v1"
	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

// fakeModerationClient menyimpan request terakhir setiap RPC.
type fakeModerationClient struct {
	err       error
	list      *moderationv1.ListReportedVideosRequest
	reports   *moderationv1.ListVideoReportsRequest
	block     *moderationv1.BlockVideoRequest
	dismiss   *moderationv1.DismissReportsRequest
	unblock   *moderationv1.UnblockVideoRequest
	blockList *moderationv1.ListBlockedVideosRequest
}

func blockedTestVideo() *moderationv1.BlockedVideo {
	v := testVideo(videov1.VideoStatus_VIDEO_STATUS_READY)
	v.Visibility = videov1.VideoVisibility_VIDEO_VISIBILITY_BLOCKED
	return &moderationv1.BlockedVideo{
		Video: v, Reason: videov1.ReportReason_REPORT_REASON_SPAM, Note: "link judi",
		ModeratorId: 7, BlockedAt: timestamppb.New(testCreatedAt),
	}
}

func (f *fakeModerationClient) ListReportedVideos(_ context.Context, in *moderationv1.ListReportedVideosRequest, _ ...grpc.CallOption) (*moderationv1.ListReportedVideosResponse, error) {
	f.list = in
	if f.err != nil {
		return nil, f.err
	}
	return &moderationv1.ListReportedVideosResponse{
		Videos: []*moderationv1.ReportedVideo{{
			Video:       testVideo(videov1.VideoStatus_VIDEO_STATUS_READY),
			OpenReports: 3,
			Reasons: []*moderationv1.ReasonCount{
				{Reason: videov1.ReportReason_REPORT_REASON_SPAM, Count: 2},
				{Reason: videov1.ReportReason_REPORT_REASON_HATE, Count: 1},
			},
			FirstReportedAt: timestamppb.New(testCreatedAt),
			LastReportedAt:  timestamppb.New(testCreatedAt),
		}},
		NextPageToken: "20",
	}, nil
}

func (f *fakeModerationClient) ListVideoReports(_ context.Context, in *moderationv1.ListVideoReportsRequest, _ ...grpc.CallOption) (*moderationv1.ListVideoReportsResponse, error) {
	f.reports = in
	if f.err != nil {
		return nil, f.err
	}
	return &moderationv1.ListVideoReportsResponse{Reports: []*moderationv1.VideoReport{{
		ReporterId: 7, Reason: videov1.ReportReason_REPORT_REASON_OTHER, Details: "judul menipu",
		ReportedAt: timestamppb.New(testCreatedAt),
	}}}, nil
}

func (f *fakeModerationClient) BlockVideo(_ context.Context, in *moderationv1.BlockVideoRequest, _ ...grpc.CallOption) (*moderationv1.BlockVideoResponse, error) {
	f.block = in
	if f.err != nil {
		return nil, f.err
	}
	return &moderationv1.BlockVideoResponse{Blocked: blockedTestVideo(), ClosedReports: 3}, nil
}

func (f *fakeModerationClient) DismissReports(_ context.Context, in *moderationv1.DismissReportsRequest, _ ...grpc.CallOption) (*moderationv1.DismissReportsResponse, error) {
	f.dismiss = in
	if f.err != nil {
		return nil, f.err
	}
	return &moderationv1.DismissReportsResponse{ClosedReports: 2}, nil
}

func (f *fakeModerationClient) UnblockVideo(_ context.Context, in *moderationv1.UnblockVideoRequest, _ ...grpc.CallOption) (*moderationv1.UnblockVideoResponse, error) {
	f.unblock = in
	if f.err != nil {
		return nil, f.err
	}
	return &moderationv1.UnblockVideoResponse{Video: testVideo(videov1.VideoStatus_VIDEO_STATUS_READY)}, nil
}

func (f *fakeModerationClient) ListBlockedVideos(_ context.Context, in *moderationv1.ListBlockedVideosRequest, _ ...grpc.CallOption) (*moderationv1.ListBlockedVideosResponse, error) {
	f.blockList = in
	if f.err != nil {
		return nil, f.err
	}
	return &moderationv1.ListBlockedVideosResponse{Videos: []*moderationv1.BlockedVideo{blockedTestVideo()}}, nil
}

func TestModerationRequiresAdmin(t *testing.T) {
	verifier, as, _ := newRoleTokenSigner(t)
	mod := &fakeModerationClient{}
	r := newTestRouter(t, Deps{Moderation: mod, Users: &fakeUserClient{}, Verifier: verifier})

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/admin/reports"},
		{http.MethodGet, "/api/v1/admin/reports/42"},
		{http.MethodGet, "/api/v1/admin/blocked"},
		{http.MethodPost, "/api/v1/admin/videos/42/block"},
		{http.MethodPost, "/api/v1/admin/videos/42/dismiss"},
		{http.MethodPost, "/api/v1/admin/videos/42/unblock"},
	} {
		assert.Equal(t, http.StatusUnauthorized, do(r, tc.method, tc.path, "").Code, tc.path)
		rec := do(r, tc.method, tc.path, `{"reason":"spam"}`, as(5))
		assert.Equal(t, http.StatusForbidden, rec.Code, tc.path)
		assert.Contains(t, rec.Body.String(), "PERMISSION_DENIED", tc.path)
	}
	assert.Nil(t, mod.list)
	assert.Nil(t, mod.block)
}

func TestModerationEndpoints(t *testing.T) {
	verifier, _, asAdmin := newRoleTokenSigner(t)
	mod := &fakeModerationClient{}
	r := newTestRouter(t, Deps{Moderation: mod, Users: &fakeUserClient{}, Verifier: verifier})

	rec := do(r, http.MethodGet, "/api/v1/admin/reports?page_size=10&page_token=20", "", asAdmin(1))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, int32(10), mod.list.GetPageSize())
	assert.Equal(t, "20", mod.list.GetPageToken())
	body := rec.Body.String()
	assert.Contains(t, body, `"open_reports":3`)
	assert.Contains(t, body, `"reasons":[{"reason":"spam","count":2},{"reason":"hate","count":1}]`)
	assert.Contains(t, body, `"owner":{"id":"7","username":"budi"`)
	assert.Contains(t, body, `"next_page_token":"20"`)

	rec = do(r, http.MethodGet, "/api/v1/admin/reports/42", "", asAdmin(1))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, int64(42), mod.reports.GetVideoId())
	assert.Contains(t, rec.Body.String(), `"reporter_id":"7","reason":"other","details":"judul menipu"`)
	assert.Contains(t, rec.Body.String(), `"reporter":{"id":"7"`)

	rec = do(r, http.MethodPost, "/api/v1/admin/videos/42/block", `{"reason":"spam","note":"link judi"}`, asAdmin(1))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, int64(1), mod.block.GetModeratorId())
	assert.Equal(t, int64(42), mod.block.GetVideoId())
	assert.Equal(t, videov1.ReportReason_REPORT_REASON_SPAM, mod.block.GetReason())
	assert.Equal(t, "link judi", mod.block.GetNote())
	assert.Contains(t, rec.Body.String(), `"closed_reports":3`)
	assert.Contains(t, rec.Body.String(), `"visibility":"BLOCKED"`)
	assert.Contains(t, rec.Body.String(), `"reason":"spam","note":"link judi"`)

	rec = do(r, http.MethodPost, "/api/v1/admin/videos/42/block", `{"reason":"bosan"}`, asAdmin(1))
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = do(r, http.MethodPost, "/api/v1/admin/videos/42/dismiss", "", asAdmin(1))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, int64(42), mod.dismiss.GetVideoId())
	assert.JSONEq(t, `{"closed_reports":2}`, rec.Body.String())

	rec = do(r, http.MethodPost, "/api/v1/admin/videos/42/unblock", "", asAdmin(1))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, int64(1), mod.unblock.GetModeratorId())

	rec = do(r, http.MethodGet, "/api/v1/admin/blocked", "", asAdmin(1))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"moderator":{"id":"7"`)

	assert.Equal(t, http.StatusNotFound, do(r, http.MethodPost, "/api/v1/admin/videos/abc/block", `{"reason":"spam"}`, asAdmin(1)).Code)

	mod.err = status.Error(codes.FailedPrecondition, "video sudah diturunkan")
	rec = do(r, http.MethodPost, "/api/v1/admin/videos/42/block", `{"reason":"spam"}`, asAdmin(1))
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "video sudah diturunkan")
}
