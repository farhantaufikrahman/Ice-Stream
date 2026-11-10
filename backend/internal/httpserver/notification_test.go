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

	userv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/user/v1"
	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

// fakeNotifications mencatat request notifikasi terakhir. Disimpan di luar
// fakeVideoClient supaya test lama tidak perlu diubah.
type fakeNotifications struct {
	list  *videov1.ListNotificationsRequest
	count *videov1.CountUnreadNotificationsRequest
	mark  *videov1.MarkNotificationsReadRequest
}

func (f *fakeVideoClient) ListNotifications(_ context.Context, in *videov1.ListNotificationsRequest, _ ...grpc.CallOption) (*videov1.ListNotificationsResponse, error) {
	f.notif.list = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.ListNotificationsResponse{
		Notifications: []*videov1.Notification{
			{Id: 9, Kind: "NEW_VIDEO", Video: testVideo(videov1.VideoStatus_VIDEO_STATUS_READY), CreatedAt: timestamppb.New(testCreatedAt)},
			{Id: 8, Kind: "NEW_VIDEO", Video: testVideo(videov1.VideoStatus_VIDEO_STATUS_READY), CreatedAt: timestamppb.New(testCreatedAt), ReadAt: timestamppb.New(testCreatedAt)},
		},
		NextPageToken: "bjg",
	}, nil
}

func (f *fakeVideoClient) CountUnreadNotifications(_ context.Context, in *videov1.CountUnreadNotificationsRequest, _ ...grpc.CallOption) (*videov1.CountUnreadNotificationsResponse, error) {
	f.notif.count = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.CountUnreadNotificationsResponse{UnreadCount: 4}, nil
}

func (f *fakeVideoClient) MarkNotificationsRead(_ context.Context, in *videov1.MarkNotificationsReadRequest, _ ...grpc.CallOption) (*videov1.MarkNotificationsReadResponse, error) {
	f.notif.mark = in
	if f.err != nil {
		return nil, f.err
	}
	if in.GetNotificationId() == 0 {
		return &videov1.MarkNotificationsReadResponse{Updated: 4}, nil
	}
	return &videov1.MarkNotificationsReadResponse{Updated: 1, UnreadCount: 3}, nil
}

func (f *fakeUserClient) ListSubscriberIds(_ context.Context, _ *userv1.ListSubscriberIdsRequest, _ ...grpc.CallOption) (*userv1.ListSubscriberIdsResponse, error) {
	// Gateway tidak pernah memanggil RPC ini (hanya VideoService).
	return nil, status.Error(codes.Unimplemented, "tidak dipakai gateway")
}

func TestListNotifications(t *testing.T) {
	verifier, as := newTokenSigner(t)
	videos, users := &fakeVideoClient{}, &fakeUserClient{}
	r := newTestRouter(t, Deps{Videos: videos, Users: users, Verifier: verifier})

	rec := do(r, http.MethodGet, "/api/v1/me/notifications?page_size=5&page_token=abc", "", as(8))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, int64(8), videos.notif.list.GetRequesterId(), "requester dari token")
	assert.Equal(t, int32(5), videos.notif.list.GetPageSize())
	assert.Equal(t, "abc", videos.notif.list.GetPageToken())
	body := rec.Body.String()
	assert.Contains(t, body, `"id":"9","kind":"NEW_VIDEO"`)
	assert.Contains(t, body, `"read":false,"created_at":"2026-10-29T04:00:00Z"`, "read_at tidak dikirim jika belum dibaca")
	assert.Contains(t, body, `"read":true,"read_at":"2026-10-29T04:00:00Z"`)
	assert.Contains(t, body, `"owner":{`, "nama channel ikut diisi")
	assert.Contains(t, body, `"next_page_token":"bjg"`)

	rec = do(r, http.MethodGet, "/api/v1/me/notifications?page_size=abc", "", as(8))
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	videos.err = status.Error(codes.InvalidArgument, "page_token: tidak valid")
	rec = do(r, http.MethodGet, "/api/v1/me/notifications?page_token=xx", "", as(8))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUnreadCountAndMarkRead(t *testing.T) {
	verifier, as := newTokenSigner(t)
	videos := &fakeVideoClient{}
	r := newTestRouter(t, Deps{Videos: videos, Verifier: verifier})

	rec := do(r, http.MethodGet, "/api/v1/me/notifications/unread-count", "", as(8))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"unread_count":4}`, rec.Body.String())
	assert.Equal(t, int64(8), videos.notif.count.GetRequesterId())

	rec = do(r, http.MethodPost, "/api/v1/me/notifications/9/read", "", as(8))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"updated":1,"unread_count":3}`, rec.Body.String())
	assert.Equal(t, int64(8), videos.notif.mark.GetRequesterId())
	assert.Equal(t, int64(9), videos.notif.mark.GetNotificationId())

	rec = do(r, http.MethodPost, "/api/v1/me/notifications/read-all", "", as(8))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"updated":4,"unread_count":0}`, rec.Body.String())
	assert.Zero(t, videos.notif.mark.GetNotificationId(), "0 = semua")

	for _, path := range []string{"/api/v1/me/notifications/abc/read", "/api/v1/me/notifications/0/read"} {
		rec = do(r, http.MethodPost, path, "", as(8))
		assert.Equal(t, http.StatusNotFound, rec.Code, path)
	}
}

func TestNotificationsRequireLogin(t *testing.T) {
	verifier, _ := newTokenSigner(t)
	r := newTestRouter(t, Deps{Videos: &fakeVideoClient{}, Verifier: verifier})
	for _, req := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/me/notifications"},
		{http.MethodGet, "/api/v1/me/notifications/unread-count"},
		{http.MethodPost, "/api/v1/me/notifications/9/read"},
		{http.MethodPost, "/api/v1/me/notifications/read-all"},
	} {
		rec := do(r, req.method, req.path, "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code, req.path)
	}
}
