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

	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

// fakePlaylists mencatat request playlist terakhir.
type fakePlaylists struct {
	create  *videov1.CreatePlaylistRequest
	update  *videov1.UpdatePlaylistRequest
	del     *videov1.DeletePlaylistRequest
	get     *videov1.GetPlaylistRequest
	list    *videov1.ListPlaylistsRequest
	add     *videov1.AddToPlaylistRequest
	remove  *videov1.RemoveFromPlaylistRequest
	entries *videov1.ListPlaylistVideosRequest
}

func testPlaylist() *videov1.Playlist {
	return &videov1.Playlist{
		Id: 5, OwnerId: 7, Title: "Belajar", Description: "Go dasar",
		Visibility:   videov1.PlaylistVisibility_PLAYLIST_VISIBILITY_PUBLIC,
		VideoCount:   2,
		ThumbnailUrl: "http://localhost:9000/videos/hls/42/thumb.jpg",
		CreatedAt:    timestamppb.New(testCreatedAt),
		UpdatedAt:    timestamppb.New(testCreatedAt),
	}
}

func (f *fakeVideoClient) CreatePlaylist(_ context.Context, in *videov1.CreatePlaylistRequest, _ ...grpc.CallOption) (*videov1.CreatePlaylistResponse, error) {
	f.pl.create = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.CreatePlaylistResponse{Playlist: testPlaylist()}, nil
}

func (f *fakeVideoClient) UpdatePlaylist(_ context.Context, in *videov1.UpdatePlaylistRequest, _ ...grpc.CallOption) (*videov1.UpdatePlaylistResponse, error) {
	f.pl.update = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.UpdatePlaylistResponse{Playlist: testPlaylist()}, nil
}

func (f *fakeVideoClient) DeletePlaylist(_ context.Context, in *videov1.DeletePlaylistRequest, _ ...grpc.CallOption) (*videov1.DeletePlaylistResponse, error) {
	f.pl.del = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.DeletePlaylistResponse{}, nil
}

func (f *fakeVideoClient) GetPlaylist(_ context.Context, in *videov1.GetPlaylistRequest, _ ...grpc.CallOption) (*videov1.GetPlaylistResponse, error) {
	f.pl.get = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.GetPlaylistResponse{Playlist: testPlaylist()}, nil
}

func (f *fakeVideoClient) ListPlaylists(_ context.Context, in *videov1.ListPlaylistsRequest, _ ...grpc.CallOption) (*videov1.ListPlaylistsResponse, error) {
	f.pl.list = in
	if f.err != nil {
		return nil, f.err
	}
	p := testPlaylist()
	p.ContainsVideo = in.GetVideoId() != 0
	return &videov1.ListPlaylistsResponse{Playlists: []*videov1.Playlist{p}, NextPageToken: "cDU"}, nil
}

func (f *fakeVideoClient) AddToPlaylist(_ context.Context, in *videov1.AddToPlaylistRequest, _ ...grpc.CallOption) (*videov1.AddToPlaylistResponse, error) {
	f.pl.add = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.AddToPlaylistResponse{AddedAt: timestamppb.New(testCreatedAt)}, nil
}

func (f *fakeVideoClient) RemoveFromPlaylist(_ context.Context, in *videov1.RemoveFromPlaylistRequest, _ ...grpc.CallOption) (*videov1.RemoveFromPlaylistResponse, error) {
	f.pl.remove = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.RemoveFromPlaylistResponse{}, nil
}

func (f *fakeVideoClient) ListPlaylistVideos(_ context.Context, in *videov1.ListPlaylistVideosRequest, _ ...grpc.CallOption) (*videov1.ListPlaylistVideosResponse, error) {
	f.pl.entries = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.ListPlaylistVideosResponse{
		Entries: []*videov1.PlaylistEntry{{
			Video:   testVideo(videov1.VideoStatus_VIDEO_STATUS_READY),
			AddedAt: timestamppb.New(testCreatedAt),
		}},
		NextPageToken: "aTE",
	}, nil
}

const testPlaylistJSON = `{"id":"5","owner_id":"7","title":"Belajar","description":"Go dasar","visibility":"PUBLIC",
	"video_count":2,"thumbnail_url":"http://localhost:9000/videos/hls/42/thumb.jpg",
	"created_at":"2026-10-29T04:00:00Z","updated_at":"2026-10-29T04:00:00Z"`

func TestCreatePlaylist(t *testing.T) {
	verifier, as := newTokenSigner(t)
	fake := &fakeVideoClient{}
	r := newTestRouter(t, Deps{Videos: fake, Verifier: verifier})

	rec := do(r, http.MethodPost, "/api/v1/playlists", `{"title":"Belajar","description":"Go dasar","visibility":"public"}`, as(8))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"playlist":`+testPlaylistJSON+`}}`, rec.Body.String())
	assert.Equal(t, int64(8), fake.pl.create.GetRequesterId(), "pemilik dari token")
	assert.Equal(t, "Belajar", fake.pl.create.GetTitle())
	assert.Equal(t, videov1.PlaylistVisibility_PLAYLIST_VISIBILITY_PUBLIC, fake.pl.create.GetVisibility())

	rec = do(r, http.MethodPost, "/api/v1/playlists", `{"title":"Rahasia"}`, as(8))
	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, videov1.PlaylistVisibility_PLAYLIST_VISIBILITY_UNSPECIFIED, fake.pl.create.GetVisibility(), "kosong = bawaan service (PRIVATE)")

	t.Run("visibilitas salah", func(t *testing.T) {
		rec := do(r, http.MethodPost, "/api/v1/playlists", `{"title":"A","visibility":"UNLISTED"}`, as(8))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
	t.Run("wajib login", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, do(r, http.MethodPost, "/api/v1/playlists", `{"title":"A"}`).Code)
	})
	t.Run("judul kosong dari service", func(t *testing.T) {
		fake.err = status.Error(codes.InvalidArgument, "title: wajib diisi, maksimal 100 karakter")
		t.Cleanup(func() { fake.err = nil })
		rec := do(r, http.MethodPost, "/api/v1/playlists", `{"title":""}`, as(8))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "title: wajib diisi")
	})
}

func TestGetPlaylist(t *testing.T) {
	verifier, as := newTokenSigner(t)
	users, fake := &fakeUserClient{}, &fakeVideoClient{}
	r := newTestRouter(t, Deps{Users: users, Videos: fake, Verifier: verifier})

	rec := do(r, http.MethodGet, "/api/v1/playlists/5", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"owner":{"id":"7","username":"budi"`)
	assert.Equal(t, int64(0), fake.pl.get.GetViewerId(), "tanpa login")
	assert.Equal(t, int64(5), fake.pl.get.GetPlaylistId())

	do(r, http.MethodGet, "/api/v1/playlists/5", "", as(8))
	assert.Equal(t, int64(8), fake.pl.get.GetViewerId(), "viewer dari token (untuk playlist PRIVATE)")

	rec = do(r, http.MethodGet, "/api/v1/playlists/5/videos?page_size=10&page_token=abc", "", as(8))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, int64(8), fake.pl.entries.GetViewerId())
	assert.Equal(t, int32(10), fake.pl.entries.GetPageSize())
	assert.Equal(t, "abc", fake.pl.entries.GetPageToken())
	assert.Contains(t, rec.Body.String(), `"entries":[{"video":{"id":"42"`)
	assert.Contains(t, rec.Body.String(), `"added_at":"2026-10-29T04:00:00Z"`)
	assert.Contains(t, rec.Body.String(), `"next_page_token":"aTE"`)

	t.Run("id bukan angka", func(t *testing.T) {
		assert.Equal(t, http.StatusNotFound, do(r, http.MethodGet, "/api/v1/playlists/abc", "").Code)
		assert.Equal(t, http.StatusNotFound, do(r, http.MethodGet, "/api/v1/playlists/0/videos", "").Code)
	})
	t.Run("privat milik orang lain", func(t *testing.T) {
		fake.err = status.Error(codes.NotFound, "playlist tidak ditemukan")
		t.Cleanup(func() { fake.err = nil })
		rec := do(r, http.MethodGet, "/api/v1/playlists/5", "")
		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Contains(t, rec.Body.String(), "playlist tidak ditemukan")
	})
}

func TestUpdateAndDeletePlaylist(t *testing.T) {
	verifier, as := newTokenSigner(t)
	fake := &fakeVideoClient{}
	r := newTestRouter(t, Deps{Videos: fake, Verifier: verifier})

	rec := do(r, http.MethodPatch, "/api/v1/playlists/5", `{"description":"","visibility":"PRIVATE"}`, as(8))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, int64(8), fake.pl.update.GetRequesterId())
	assert.Equal(t, int64(5), fake.pl.update.GetPlaylistId())
	assert.Nil(t, fake.pl.update.Title, "tidak dikirim = tidak diubah")
	require.NotNil(t, fake.pl.update.Description)
	assert.Empty(t, fake.pl.update.GetDescription(), "string kosong tetap dikirim")
	assert.Equal(t, videov1.PlaylistVisibility_PLAYLIST_VISIBILITY_PRIVATE, fake.pl.update.GetVisibility())

	rec = do(r, http.MethodPatch, "/api/v1/playlists/5", `{"visibility":""}`, as(8))
	assert.Equal(t, http.StatusBadRequest, rec.Code, "visibility kosong bukan nilai yang sah")

	rec = do(r, http.MethodDelete, "/api/v1/playlists/5", "", as(8))
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, int64(8), fake.pl.del.GetRequesterId())

	t.Run("wajib login", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, do(r, http.MethodPatch, "/api/v1/playlists/5", `{"title":"A"}`).Code)
		assert.Equal(t, http.StatusUnauthorized, do(r, http.MethodDelete, "/api/v1/playlists/5", "").Code)
	})
}

func TestPlaylistVideos(t *testing.T) {
	verifier, as := newTokenSigner(t)
	fake := &fakeVideoClient{}
	r := newTestRouter(t, Deps{Videos: fake, Verifier: verifier})

	rec := do(r, http.MethodPut, "/api/v1/playlists/5/videos/42", "", as(8))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"saved":true,"added_at":"2026-10-29T04:00:00Z"}`, rec.Body.String())
	assert.Equal(t, int64(8), fake.pl.add.GetRequesterId())
	assert.Equal(t, int64(5), fake.pl.add.GetPlaylistId())
	assert.Equal(t, int64(42), fake.pl.add.GetVideoId())

	rec = do(r, http.MethodDelete, "/api/v1/playlists/5/videos/42", "", as(8))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"saved":false}`, rec.Body.String())
	assert.Equal(t, int64(42), fake.pl.remove.GetVideoId())

	assert.Equal(t, http.StatusNotFound, do(r, http.MethodPut, "/api/v1/playlists/5/videos/abc", "", as(8)).Code)
	assert.Equal(t, http.StatusUnauthorized, do(r, http.MethodPut, "/api/v1/playlists/5/videos/42", "").Code)
}

func TestListPlaylists(t *testing.T) {
	verifier, as := newTokenSigner(t)
	users, fake := &fakeUserClient{}, &fakeVideoClient{}
	r := newTestRouter(t, Deps{Users: users, Videos: fake, Verifier: verifier})

	t.Run("milik sendiri dengan video_id", func(t *testing.T) {
		rec := do(r, http.MethodGet, "/api/v1/me/playlists?video_id=42&page_size=50", "", as(8))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.JSONEq(t, `{"playlists":[`+testPlaylistJSON+`,"contains_video":true}],"next_page_token":"cDU"}`, rec.Body.String())
		assert.Equal(t, int64(8), fake.pl.list.GetViewerId())
		assert.Equal(t, int64(8), fake.pl.list.GetOwnerId())
		assert.Equal(t, int64(42), fake.pl.list.GetVideoId())
		assert.Equal(t, int32(50), fake.pl.list.GetPageSize())

		assert.Equal(t, http.StatusBadRequest, do(r, http.MethodGet, "/api/v1/me/playlists?video_id=x", "", as(8)).Code)
		assert.Equal(t, http.StatusUnauthorized, do(r, http.MethodGet, "/api/v1/me/playlists", "").Code)
	})
	t.Run("kanal orang lain", func(t *testing.T) {
		rec := do(r, http.MethodGet, "/api/v1/users/budi/playlists?page_token=cDU", "", as(8))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.NotContains(t, rec.Body.String(), "contains_video")
		assert.Equal(t, int64(8), fake.pl.list.GetViewerId())
		assert.Equal(t, int64(7), fake.pl.list.GetOwnerId(), "id dari profil, bukan dari URL")
		assert.Equal(t, int64(0), fake.pl.list.GetVideoId())
		assert.Equal(t, "cDU", fake.pl.list.GetPageToken())

		do(r, http.MethodGet, "/api/v1/users/budi/playlists", "")
		assert.Equal(t, int64(0), fake.pl.list.GetViewerId(), "tanpa login")
	})
}

func TestGetVideoViewerInPlaylist(t *testing.T) {
	verifier, as := newTokenSigner(t)
	r := newTestRouter(t, Deps{Videos: &fakeVideoClient{}, Verifier: verifier})

	rec := do(r, http.MethodGet, "/api/v1/videos/42", "", as(8))
	assert.Contains(t, rec.Body.String(), `"viewer_in_playlist":true`)
	rec = do(r, http.MethodGet, "/api/v1/videos/42", "", as(9))
	assert.Contains(t, rec.Body.String(), `"viewer_in_playlist":false`)
	rec = do(r, http.MethodGet, "/api/v1/videos/42", "")
	assert.NotContains(t, rec.Body.String(), "viewer_in_playlist", "tanpa login tidak dikirim")
}
