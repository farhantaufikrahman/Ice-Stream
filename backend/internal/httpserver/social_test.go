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

	userv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/user/v1"
	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

func (f *fakeVideoClient) LikeVideo(_ context.Context, in *videov1.LikeVideoRequest, _ ...grpc.CallOption) (*videov1.LikeVideoResponse, error) {
	f.lastLike = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.LikeVideoResponse{Liked: true, LikeCount: 4}, nil
}

func (f *fakeVideoClient) UnlikeVideo(_ context.Context, in *videov1.UnlikeVideoRequest, _ ...grpc.CallOption) (*videov1.UnlikeVideoResponse, error) {
	f.lastUnlike = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.UnlikeVideoResponse{Liked: false, LikeCount: 3}, nil
}

func (f *fakeUserClient) Subscribe(_ context.Context, in *userv1.SubscribeRequest, _ ...grpc.CallOption) (*userv1.SubscribeResponse, error) {
	f.lastSubscribe = in
	if f.err != nil {
		return nil, f.err
	}
	return &userv1.SubscribeResponse{Subscribed: true, SubscriberCount: 13}, nil
}

func (f *fakeUserClient) Unsubscribe(_ context.Context, in *userv1.UnsubscribeRequest, _ ...grpc.CallOption) (*userv1.UnsubscribeResponse, error) {
	f.lastUnsub = in
	if f.err != nil {
		return nil, f.err
	}
	return &userv1.UnsubscribeResponse{Subscribed: false, SubscriberCount: 12}, nil
}

func (f *fakeUserClient) ListSubscriptions(_ context.Context, in *userv1.ListSubscriptionsRequest, _ ...grpc.CallOption) (*userv1.ListSubscriptionsResponse, error) {
	f.lastListSubs = in
	if f.err != nil {
		return nil, f.err
	}
	return &userv1.ListSubscriptionsResponse{Channels: []*userv1.Profile{testProfile(false)}, NextPageToken: "Mw"}, nil
}

func (f *fakeUserClient) ListSubscribedChannelIds(_ context.Context, in *userv1.ListSubscribedChannelIdsRequest, _ ...grpc.CallOption) (*userv1.ListSubscribedChannelIdsResponse, error) {
	f.lastChannelIDs = in
	if f.err != nil {
		return nil, f.err
	}
	return &userv1.ListSubscribedChannelIdsResponse{ChannelIds: f.channelIDs}, nil
}

func TestLikeVideo(t *testing.T) {
	verifier, as := newTokenSigner(t)
	videos := &fakeVideoClient{}
	r := newTestRouter(t, Deps{Videos: videos, Verifier: verifier})

	rec := do(r, http.MethodPut, "/api/v1/videos/42/like", "", as(8))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"liked":true,"like_count":4}`, rec.Body.String())
	assert.Equal(t, int64(8), videos.lastLike.GetRequesterId(), "dari access token")
	assert.Equal(t, int64(42), videos.lastLike.GetVideoId())

	rec = do(r, http.MethodDelete, "/api/v1/videos/42/like", "", as(8))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"liked":false,"like_count":3}`, rec.Body.String())

	rec = do(r, http.MethodPut, "/api/v1/videos/42/like", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "wajib login")

	videos.err = status.Error(codes.NotFound, "video tidak ditemukan")
	rec = do(r, http.MethodPut, "/api/v1/videos/43/like", "", as(8))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetVideoViewerLiked(t *testing.T) {
	verifier, as := newTokenSigner(t)
	r := newTestRouter(t, Deps{Videos: &fakeVideoClient{}, Verifier: verifier})

	rec := do(r, http.MethodGet, "/api/v1/videos/42", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), "viewer_liked", "tanpa login tidak dikirim")

	rec = do(r, http.MethodGet, "/api/v1/videos/42", "", as(8))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"viewer_liked":false`)
}

func TestSubscribe(t *testing.T) {
	verifier, as := newTokenSigner(t)
	users := &fakeUserClient{}
	r := newTestRouter(t, Deps{Users: users, Verifier: verifier})

	rec := do(r, http.MethodPut, "/api/v1/users/budi/subscription", "", as(8))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"subscribed":true,"subscriber_count":13}`, rec.Body.String())
	assert.Equal(t, int64(8), users.lastSubscribe.GetRequesterId())
	assert.Equal(t, int64(7), users.lastSubscribe.GetChannelId(), "id dari profil, bukan dari URL")

	rec = do(r, http.MethodDelete, "/api/v1/users/budi/subscription", "", as(8))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"subscribed":false,"subscriber_count":12}`, rec.Body.String())

	rec = do(r, http.MethodPut, "/api/v1/users/budi/subscription", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	users.err = status.Error(codes.FailedPrecondition, "kamu sudah mengikuti 1000 channel")
	rec = do(r, http.MethodPut, "/api/v1/users/budi/subscription", "", as(8))
	assert.Equal(t, http.StatusConflict, rec.Code, "batas 1000 channel = 409 FAILED_PRECONDITION")
}

func TestChannelViewerSubscribed(t *testing.T) {
	verifier, as := newTokenSigner(t)
	users := &fakeUserClient{subscribed: true}
	r := newTestRouter(t, Deps{Users: users, Verifier: verifier})

	rec := do(r, http.MethodGet, "/api/v1/users/budi", "", as(8))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"viewer_subscribed":true`)
	assert.Equal(t, int64(8), users.lastGet.GetViewerId())

	// Melihat channel sendiri: UserService mengisi email, gateway membuangnya.
	rec = do(r, http.MethodGet, "/api/v1/users/budi", "", as(7))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), "email")
}

func TestMySubscriptions(t *testing.T) {
	verifier, as := newTokenSigner(t)
	users := &fakeUserClient{}
	r := newTestRouter(t, Deps{Users: users, Verifier: verifier})

	rec := do(r, http.MethodGet, "/api/v1/me/subscriptions?page_size=5", "", as(8))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"channels":[{"id":"7","username":"budi"`)
	assert.Contains(t, rec.Body.String(), `"next_page_token":"Mw"`)
	assert.Equal(t, int64(8), users.lastListSubs.GetRequesterId())
	assert.Equal(t, int32(5), users.lastListSubs.GetPageSize())

	rec = do(r, http.MethodGet, "/api/v1/me/subscriptions", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestSubscriptionFeed(t *testing.T) {
	verifier, as := newTokenSigner(t)
	users, videos := &fakeUserClient{}, &fakeVideoClient{}
	r := newTestRouter(t, Deps{Users: users, Videos: videos, Verifier: verifier})

	// Belum subscribe siapa pun: feed kosong, VideoService tidak dipanggil.
	rec := do(r, http.MethodGet, "/api/v1/me/feed", "", as(8))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"videos":[],"next_page_token":""}`, rec.Body.String())
	assert.Nil(t, videos.lastFeed)

	users.channelIDs = []int64{7, 9}
	rec = do(r, http.MethodGet, "/api/v1/me/feed?page_size=3&page_token=abc", "", as(8))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []int64{7, 9}, videos.lastFeed.GetOwnerIds())
	assert.Zero(t, videos.lastFeed.GetOwnerId())
	assert.Equal(t, int32(3), videos.lastFeed.GetPageSize())
	assert.Equal(t, "abc", videos.lastFeed.GetPageToken())
	assert.Contains(t, rec.Body.String(), `"owner":{"id":"7"`)

	rec = do(r, http.MethodGet, "/api/v1/me/feed", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
