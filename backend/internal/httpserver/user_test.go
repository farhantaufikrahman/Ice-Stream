package httpserver

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	userv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/user/v1"
)

// fakeUserClient menggantikan UserService sungguhan di test.
type fakeUserClient struct {
	err      error
	batchErr error

	lastGet    *userv1.GetProfileRequest
	lastBatch  *userv1.BatchGetProfilesRequest
	lastUpdate *userv1.UpdateProfileRequest
	lastUpload *userv1.UploadAvatarRequest
}

var _ userv1.UserServiceClient = (*fakeUserClient)(nil)

func testProfile(withEmail bool) *userv1.Profile {
	p := &userv1.Profile{
		Id: 7, Username: "budi", DisplayName: "Budi", Bio: "Suka kucing",
		AvatarUrl: "http://media.ice-stream.localhost:4000/images/avatars/7/ab.jpg",
		CreatedAt: timestamppb.New(testCreatedAt),
	}
	if withEmail {
		p.Email = "budi@example.com"
	}
	return p
}

func (f *fakeUserClient) GetProfile(_ context.Context, in *userv1.GetProfileRequest, _ ...grpc.CallOption) (*userv1.GetProfileResponse, error) {
	f.lastGet = in
	if f.err != nil {
		return nil, f.err
	}
	return &userv1.GetProfileResponse{Profile: testProfile(in.GetViewerId() == 7)}, nil
}

func (f *fakeUserClient) BatchGetProfiles(_ context.Context, in *userv1.BatchGetProfilesRequest, _ ...grpc.CallOption) (*userv1.BatchGetProfilesResponse, error) {
	f.lastBatch = in
	if f.batchErr != nil {
		return nil, f.batchErr
	}
	return &userv1.BatchGetProfilesResponse{Profiles: []*userv1.Profile{testProfile(false)}}, nil
}

func (f *fakeUserClient) UpdateProfile(_ context.Context, in *userv1.UpdateProfileRequest, _ ...grpc.CallOption) (*userv1.UpdateProfileResponse, error) {
	f.lastUpdate = in
	if f.err != nil {
		return nil, f.err
	}
	return &userv1.UpdateProfileResponse{Profile: testProfile(true)}, nil
}

func (f *fakeUserClient) UploadAvatar(_ context.Context, in *userv1.UploadAvatarRequest, _ ...grpc.CallOption) (*userv1.UploadAvatarResponse, error) {
	f.lastUpload = in
	if f.err != nil {
		return nil, f.err
	}
	return &userv1.UploadAvatarResponse{Profile: testProfile(true)}, nil
}

func (f *fakeUserClient) RemoveAvatar(_ context.Context, _ *userv1.RemoveAvatarRequest, _ ...grpc.CallOption) (*userv1.RemoveAvatarResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	p := testProfile(true)
	p.AvatarUrl = ""
	return &userv1.RemoveAvatarResponse{Profile: p}, nil
}

func TestGetUserByUsername(t *testing.T) {
	users := &fakeUserClient{}
	r := newTestRouter(t, Deps{Users: users})

	rec := do(r, http.MethodGet, "/api/v1/users/budi", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"user":{"id":"7","username":"budi","display_name":"Budi","bio":"Suka kucing",
		"avatar_url":"http://media.ice-stream.localhost:4000/images/avatars/7/ab.jpg",
		"created_at":"2026-10-29T04:00:00Z"}}`, rec.Body.String(), "tanpa email")
	assert.Equal(t, "budi", users.lastGet.GetUsername())

	users.err = status.Error(codes.NotFound, "user tidak ditemukan")
	rec = do(r, http.MethodGet, "/api/v1/users/tidakada", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestListUserVideos(t *testing.T) {
	users, videos := &fakeUserClient{}, &fakeVideoClient{}
	r := newTestRouter(t, Deps{Users: users, Videos: videos})

	rec := do(r, http.MethodGet, "/api/v1/users/budi/videos?page_size=5&page_token=xyz", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, int64(7), videos.lastFeed.GetOwnerId(), "id dari profil, bukan dari URL")
	assert.Equal(t, int32(5), videos.lastFeed.GetPageSize())
	assert.Equal(t, "xyz", videos.lastFeed.GetPageToken())
	assert.Contains(t, rec.Body.String(), `"owner":{"id":"7","username":"budi","display_name":"Budi"`)
	assert.Nil(t, users.lastBatch, "pemilik sudah diketahui, tidak perlu BatchGetProfiles")

	users.err = status.Error(codes.NotFound, "user tidak ditemukan")
	rec = do(r, http.MethodGet, "/api/v1/users/tidakada/videos", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestFeedAndDetailIncludeOwner(t *testing.T) {
	users, videos := &fakeUserClient{}, &fakeVideoClient{}
	r := newTestRouter(t, Deps{Users: users, Videos: videos})

	rec := do(r, http.MethodGet, "/api/v1/videos", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"owner":{"id":"7","username":"budi","display_name":"Budi","avatar_url":"http://media.ice-stream.localhost:4000/images/avatars/7/ab.jpg"}`)
	assert.Equal(t, []int64{7}, users.lastBatch.GetUserIds())

	rec = do(r, http.MethodGet, "/api/v1/videos/42", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"owner":{"id":"7"`)

	// UserService mati: feed tetap tampil, hanya tanpa info pemilik.
	users.batchErr = status.Error(codes.Unavailable, "mati")
	rec = do(r, http.MethodGet, "/api/v1/videos", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), `"owner"`)
	assert.Contains(t, rec.Body.String(), `"hls_url"`)
}

func TestMyProfile(t *testing.T) {
	verifier, as := newTokenSigner(t)
	users := &fakeUserClient{}
	r := newTestRouter(t, Deps{Users: users, Verifier: verifier})

	rec := do(r, http.MethodGet, "/api/v1/me/profile", "", as(7))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"email":"budi@example.com"`)
	assert.Equal(t, int64(7), users.lastGet.GetUserId())
	assert.Equal(t, int64(7), users.lastGet.GetViewerId())

	rec = do(r, http.MethodGet, "/api/v1/me/profile", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestUpdateMyProfile(t *testing.T) {
	verifier, as := newTokenSigner(t)
	users := &fakeUserClient{}
	r := newTestRouter(t, Deps{Users: users, Verifier: verifier})

	// Hanya bio yang dikirim: display_name tetap nil (tidak diubah).
	rec := do(r, http.MethodPatch, "/api/v1/me/profile", `{"bio":""}`, as(7))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, int64(7), users.lastUpdate.GetRequesterId())
	assert.Nil(t, users.lastUpdate.DisplayName)
	require.NotNil(t, users.lastUpdate.Bio)
	assert.Equal(t, "", *users.lastUpdate.Bio)

	rec = do(r, http.MethodPatch, "/api/v1/me/profile", `bukan json`, as(7))
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	users.err = status.Error(codes.InvalidArgument, "display_name: wajib diisi, maksimal 50 karakter")
	rec = do(r, http.MethodPatch, "/api/v1/me/profile", `{"display_name":""}`, as(7))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "display_name")
}

func TestUploadAvatar(t *testing.T) {
	verifier, as := newTokenSigner(t)
	users := &fakeUserClient{}
	r := newTestRouter(t, Deps{Users: users, Verifier: verifier})

	img := []byte("\x89PNG\r\n\x1a\n...isi gambar...")
	req := httptest.NewRequest(http.MethodPut, "/api/v1/me/avatar", bytes.NewReader(img))
	req.Header.Set("Content-Type", "image/png")
	as(7)(req)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, img, users.lastUpload.GetImage(), "isi file diteruskan apa adanya")
	assert.Equal(t, int64(7), users.lastUpload.GetRequesterId())

	// Lebih dari 2 MiB: ditolak gateway, UserService tidak dipanggil.
	users.lastUpload = nil
	req = httptest.NewRequest(http.MethodPut, "/api/v1/me/avatar", bytes.NewReader(make([]byte, maxAvatarBodyBytes+1)))
	as(7)(req)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Nil(t, users.lastUpload)

	rec = do(r, http.MethodPut, "/api/v1/me/avatar", "x")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = do(r, http.MethodDelete, "/api/v1/me/avatar", "", as(7))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"avatar_url":""`)
}
