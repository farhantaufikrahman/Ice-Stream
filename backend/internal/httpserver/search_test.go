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

func (f *fakeUserClient) SearchUsers(_ context.Context, in *userv1.SearchUsersRequest, _ ...grpc.CallOption) (*userv1.SearchUsersResponse, error) {
	f.lastSearch = in
	if f.err != nil {
		return nil, f.err
	}
	other := testProfile(false)
	other.Id, other.Username, other.DisplayName = 8, "budiman", "Budiman"
	return &userv1.SearchUsersResponse{Profiles: []*userv1.Profile{testProfile(false), other}, NextPageToken: "czE"}, nil
}

func (f *fakeVideoClient) SearchPlaylists(_ context.Context, in *videov1.SearchPlaylistsRequest, _ ...grpc.CallOption) (*videov1.SearchPlaylistsResponse, error) {
	f.pl.search = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.SearchPlaylistsResponse{Playlists: []*videov1.Playlist{testPlaylist()}, NextPageToken: "czE"}, nil
}

func TestSearchChannels(t *testing.T) {
	verifier, as := newTokenSigner(t)
	users := &fakeUserClient{channelIDs: []int64{8}}
	r := newTestRouter(t, Deps{Users: users, Videos: &fakeVideoClient{}, Verifier: verifier})

	rec := do(r, http.MethodGet, "/api/v1/search/channels?q=budi&page_size=5&page_token=abc", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "budi", users.lastSearch.GetQuery())
	assert.Equal(t, int32(5), users.lastSearch.GetPageSize())
	assert.Equal(t, "abc", users.lastSearch.GetPageToken())
	assert.Contains(t, rec.Body.String(), `"channels":[{"id":"7","username":"budi"`)
	assert.Contains(t, rec.Body.String(), `"subscriber_count":12`)
	assert.Contains(t, rec.Body.String(), `"next_page_token":"czE"`)
	assert.NotContains(t, rec.Body.String(), "viewer_subscribed", "tamu: tanpa status mengikuti")
	assert.NotContains(t, rec.Body.String(), "email")

	rec = do(r, http.MethodGet, "/api/v1/search/channels?q=budi", "", as(9))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, int64(9), users.lastChannelIDs.GetRequesterId())
	assert.Contains(t, rec.Body.String(), `"username":"budi"`)
	assert.Contains(t, rec.Body.String(), `"viewer_subscribed":false`)
	assert.Contains(t, rec.Body.String(), `"username":"budiman","display_name":"Budiman"`)
	assert.Contains(t, rec.Body.String(), `"viewer_subscribed":true`)

	t.Run("tanpa q", func(t *testing.T) {
		assert.Equal(t, http.StatusBadRequest, do(r, http.MethodGet, "/api/v1/search/channels", "").Code)
	})
	t.Run("query ditolak UserService", func(t *testing.T) {
		users.err = status.Error(codes.InvalidArgument, "query: isi minimal satu kata dengan 2 huruf atau lebih")
		t.Cleanup(func() { users.err = nil })
		assert.Equal(t, http.StatusBadRequest, do(r, http.MethodGet, "/api/v1/search/channels?q=a", "").Code)
	})
}

func TestSearchPlaylists(t *testing.T) {
	users, fake := &fakeUserClient{}, &fakeVideoClient{}
	r := newTestRouter(t, Deps{Users: users, Videos: fake})

	rec := do(r, http.MethodGet, "/api/v1/search/playlists?q=belajar&page_token=abc", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "belajar", fake.pl.search.GetQuery())
	assert.Equal(t, "abc", fake.pl.search.GetPageToken())
	assert.Contains(t, rec.Body.String(), `"playlists":[{"id":"5","owner_id":"7","title":"Belajar"`)
	assert.Contains(t, rec.Body.String(), `"owner":{"id":"7","username":"budi"`)
	assert.Contains(t, rec.Body.String(), `"next_page_token":"czE"`)

	assert.Equal(t, http.StatusBadRequest, do(r, http.MethodGet, "/api/v1/search/playlists?q=", "").Code)
	assert.Equal(t, http.StatusBadRequest, do(r, http.MethodGet, "/api/v1/search/playlists?q=go&page_size=x", "").Code)
}
