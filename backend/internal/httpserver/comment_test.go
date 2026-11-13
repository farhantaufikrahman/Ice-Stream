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

	commentv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/comment/v1"
)

// fakeCommentClient menggantikan CommentService sungguhan di test.
type fakeCommentClient struct {
	err error

	lastCreate  *commentv1.CreateCommentRequest
	lastList    *commentv1.ListCommentsRequest
	lastReplies *commentv1.ListRepliesRequest
	lastDelete  *commentv1.DeleteCommentRequest
	lastChannel *commentv1.ListChannelCommentsRequest
}

var _ commentv1.CommentServiceClient = (*fakeCommentClient)(nil)

func testComment(id, parentID int64) *commentv1.Comment {
	return &commentv1.Comment{
		Id: id, VideoId: 42, AuthorId: 7, ParentId: parentID, Body: "Keren!",
		CreatedAt: timestamppb.New(testCreatedAt),
	}
}

func (f *fakeCommentClient) CreateComment(_ context.Context, in *commentv1.CreateCommentRequest, _ ...grpc.CallOption) (*commentv1.CreateCommentResponse, error) {
	f.lastCreate = in
	if f.err != nil {
		return nil, f.err
	}
	c := testComment(10, in.GetParentId())
	c.Body = in.GetBody()
	return &commentv1.CreateCommentResponse{Comment: c}, nil
}

func (f *fakeCommentClient) ListComments(_ context.Context, in *commentv1.ListCommentsRequest, _ ...grpc.CallOption) (*commentv1.ListCommentsResponse, error) {
	f.lastList = in
	if f.err != nil {
		return nil, f.err
	}
	root := testComment(10, 0)
	root.ReplyCount = 2
	return &commentv1.ListCommentsResponse{Comments: []*commentv1.Comment{root, testComment(9, 0)}, NextPageToken: "OQ"}, nil
}

func (f *fakeCommentClient) ListReplies(_ context.Context, in *commentv1.ListRepliesRequest, _ ...grpc.CallOption) (*commentv1.ListRepliesResponse, error) {
	f.lastReplies = in
	if f.err != nil {
		return nil, f.err
	}
	return &commentv1.ListRepliesResponse{Comments: []*commentv1.Comment{testComment(11, 10)}}, nil
}

func (f *fakeCommentClient) DeleteComment(_ context.Context, in *commentv1.DeleteCommentRequest, _ ...grpc.CallOption) (*commentv1.DeleteCommentResponse, error) {
	f.lastDelete = in
	if f.err != nil {
		return nil, f.err
	}
	return &commentv1.DeleteCommentResponse{}, nil
}

func TestListComments(t *testing.T) {
	comments, users := &fakeCommentClient{}, &fakeUserClient{}
	r := newTestRouter(t, Deps{Comments: comments, Users: users})

	rec := do(r, http.MethodGet, "/api/v1/videos/42/comments?page_size=2&page_token=abc", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"comments":[
		{"id":"10","video_id":"42","author_id":"7","body":"Keren!","reply_count":2,"created_at":"2026-10-29T04:00:00Z",
		 "author":{"id":"7","username":"budi","display_name":"Budi","avatar_url":"http://media.ice-stream.localhost:4000/images/avatars/7/ab.jpg"}},
		{"id":"9","video_id":"42","author_id":"7","body":"Keren!","reply_count":0,"created_at":"2026-10-29T04:00:00Z",
		 "author":{"id":"7","username":"budi","display_name":"Budi","avatar_url":"http://media.ice-stream.localhost:4000/images/avatars/7/ab.jpg"}}
	],"next_page_token":"OQ"}`, rec.Body.String())
	assert.Equal(t, int64(42), comments.lastList.GetVideoId())
	assert.Equal(t, int32(2), comments.lastList.GetPageSize())
	assert.Equal(t, "abc", comments.lastList.GetPageToken())
	assert.Equal(t, []int64{7}, users.lastBatch.GetUserIds(), "penulis yang sama cukup diminta sekali")

	// UserService mati: komentar tetap tampil tanpa info penulis.
	users.batchErr = status.Error(codes.Unavailable, "mati")
	rec = do(r, http.MethodGet, "/api/v1/videos/42/comments", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), `"author"`)

	rec = do(r, http.MethodGet, "/api/v1/videos/abc/comments", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)

	comments.err = status.Error(codes.NotFound, "video tidak ditemukan")
	rec = do(r, http.MethodGet, "/api/v1/videos/43/comments", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestListReplies(t *testing.T) {
	comments := &fakeCommentClient{}
	r := newTestRouter(t, Deps{Comments: comments})

	rec := do(r, http.MethodGet, "/api/v1/videos/42/comments/10/replies", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"parent_id":"10"`)
	assert.Equal(t, int64(10), comments.lastReplies.GetCommentId())
	assert.Equal(t, int64(42), comments.lastReplies.GetVideoId())

	rec = do(r, http.MethodGet, "/api/v1/videos/42/comments/x/replies", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestCreateComment(t *testing.T) {
	verifier, as := newTokenSigner(t)
	comments := &fakeCommentClient{}
	r := newTestRouter(t, Deps{Comments: comments, Verifier: verifier})

	rec := do(r, http.MethodPost, "/api/v1/videos/42/comments", `{"body":"Halo"}`, as(8))
	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Contains(t, rec.Body.String(), `"comment":{"id":"10"`)
	assert.NotContains(t, rec.Body.String(), `"parent_id"`, "komentar utama")
	assert.Equal(t, int64(8), comments.lastCreate.GetRequesterId(), "penulis dari access token")
	assert.Equal(t, int64(0), comments.lastCreate.GetParentId())

	rec = do(r, http.MethodPost, "/api/v1/videos/42/comments", `{"body":"Setuju","parent_id":"10"}`, as(8))
	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, int64(10), comments.lastCreate.GetParentId())

	rec = do(r, http.MethodPost, "/api/v1/videos/42/comments", `{"body":"x","parent_id":"abc"}`, as(8))
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = do(r, http.MethodPost, "/api/v1/videos/42/comments", `{"body":"Halo"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "wajib login")

	comments.err = status.Error(codes.InvalidArgument, "body: wajib diisi, maksimal 1000 karakter")
	rec = do(r, http.MethodPost, "/api/v1/videos/42/comments", `{"body":""}`, as(8))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "maksimal 1000 karakter")
}

func TestDeleteComment(t *testing.T) {
	verifier, as := newTokenSigner(t)
	comments := &fakeCommentClient{}
	r := newTestRouter(t, Deps{Comments: comments, Verifier: verifier})

	rec := do(r, http.MethodDelete, "/api/v1/videos/42/comments/10", "", as(7))
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())
	assert.Equal(t, int64(7), comments.lastDelete.GetRequesterId())
	assert.Equal(t, int64(10), comments.lastDelete.GetCommentId())

	comments.err = status.Error(codes.PermissionDenied, "kamu tidak boleh menghapus komentar ini")
	rec = do(r, http.MethodDelete, "/api/v1/videos/42/comments/10", "", as(8))
	assert.Equal(t, http.StatusForbidden, rec.Code)

	rec = do(r, http.MethodDelete, "/api/v1/videos/42/comments/10", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
