package comment

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commentv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/comment/v1"
)

// fakeVideo adalah video di fakeRepo.
type fakeVideo struct {
	ownerID      int64
	ready        bool
	commentCount int64
}

// fakeRepo menyimpan komentar di memori, menggantikan Oracle saat test.
// Aturannya dibuat sama dengan OracleRepository, termasuk penghitungnya.
type fakeRepo struct {
	mu       sync.Mutex
	videos   map[int64]*fakeVideo
	comments map[int64]*Comment
	deleted  map[int64]bool
	nextID   int64
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		videos: map[int64]*fakeVideo{
			1: {ownerID: 100, ready: true},
			2: {ownerID: 100, ready: true},
			3: {ownerID: 100, ready: false}, // masih PROCESSING
		},
		comments: map[int64]*Comment{},
		deleted:  map[int64]bool{},
	}
}

func (r *fakeRepo) CheckVideo(_ context.Context, videoID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if v, ok := r.videos[videoID]; !ok || !v.ready {
		return ErrVideoNotFound
	}
	return nil
}

func (r *fakeRepo) live(id, videoID int64) (*Comment, bool) {
	c, ok := r.comments[id]
	if !ok || r.deleted[id] || c.VideoID != videoID {
		return nil, false
	}
	return c, true
}

func (r *fakeRepo) Create(_ context.Context, nc NewComment) (Comment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.videos[nc.VideoID]
	if !ok || !v.ready {
		return Comment{}, ErrVideoNotFound
	}
	c := Comment{VideoID: nc.VideoID, AuthorID: nc.AuthorID, Body: nc.Body, CreatedAt: time.Now().UTC()}
	if nc.ParentID > 0 {
		parent, ok := r.live(nc.ParentID, nc.VideoID)
		if !ok {
			return Comment{}, ErrNotFound
		}
		root := parent
		if !parent.IsRoot() {
			root = r.comments[parent.ParentID]
		}
		root.ReplyCount++
		c.ParentID = root.ID
	}
	r.nextID++
	c.ID = r.nextID
	r.comments[c.ID] = &c
	v.commentCount++
	return c, nil
}

func (r *fakeRepo) FindRoot(_ context.Context, videoID, commentID int64) (Comment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.live(commentID, videoID)
	if !ok || !c.IsRoot() {
		return Comment{}, ErrNotFound
	}
	return *c, nil
}

func (r *fakeRepo) ListRoots(_ context.Context, videoID, beforeID int64, limit int) ([]Comment, error) {
	return r.filter(func(c *Comment) bool {
		return c.VideoID == videoID && c.IsRoot() && (beforeID == 0 || c.ID < beforeID)
	}, true, limit), nil
}

func (r *fakeRepo) ListReplies(_ context.Context, videoID, rootID, afterID int64, limit int) ([]Comment, error) {
	return r.filter(func(c *Comment) bool {
		return c.VideoID == videoID && c.ParentID == rootID && c.ID > afterID
	}, false, limit), nil
}

func (r *fakeRepo) filter(keep func(*Comment) bool, desc bool, limit int) []Comment {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Comment
	for id, c := range r.comments {
		if !r.deleted[id] && keep(c) {
			out = append(out, *c)
		}
	}
	slices.SortFunc(out, func(a, b Comment) int {
		if desc {
			return int(b.ID - a.ID)
		}
		return int(a.ID - b.ID)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (r *fakeRepo) Delete(_ context.Context, videoID, commentID int64, _ time.Time, authorize func(Comment, int64) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.videos[videoID]
	if !ok || !v.ready {
		return ErrVideoNotFound
	}
	c, ok := r.live(commentID, videoID)
	if !ok {
		return ErrNotFound
	}
	if err := authorize(*c, v.ownerID); err != nil {
		return err
	}
	r.deleted[c.ID] = true
	v.commentCount--
	if c.IsRoot() {
		for id, reply := range r.comments {
			if reply.ParentID == c.ID && !r.deleted[id] {
				r.deleted[id] = true
				v.commentCount--
			}
		}
	} else {
		r.comments[c.ParentID].ReplyCount--
	}
	return nil
}

func newTestService() (*Service, *fakeRepo) {
	repo := newFakeRepo()
	return NewService(repo), repo
}

func TestCreateComment(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTestService()

	c, err := svc.CreateComment(ctx, 7, 1, 0, "  Halo\r\nsemua  ")
	require.NoError(t, err)
	assert.Equal(t, "Halo\nsemua", c.Body, "spasi dibuang dan \\r\\n menjadi \\n")
	assert.Equal(t, int64(7), c.AuthorID)
	assert.True(t, c.IsRoot())
	assert.Equal(t, int64(1), repo.videos[1].commentCount)

	t.Run("video belum READY", func(t *testing.T) {
		_, err := svc.CreateComment(ctx, 7, 3, 0, "halo")
		assert.ErrorIs(t, err, ErrVideoNotFound)
	})
	t.Run("video tidak ada", func(t *testing.T) {
		_, err := svc.CreateComment(ctx, 7, 99, 0, "halo")
		assert.ErrorIs(t, err, ErrVideoNotFound)
	})
	t.Run("requester kosong adalah bug gateway", func(t *testing.T) {
		_, err := svc.CreateComment(ctx, 0, 1, 0, "halo")
		require.Error(t, err)
		var verr *ValidationError
		assert.NotErrorAs(t, err, &verr)
	})
}

func TestCreateReply(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTestService()

	root, err := svc.CreateComment(ctx, 7, 1, 0, "komentar utama")
	require.NoError(t, err)
	reply, err := svc.CreateComment(ctx, 8, 1, root.ID, "balasan")
	require.NoError(t, err)
	assert.Equal(t, root.ID, reply.ParentID)

	// Membalas sebuah balasan tetap disimpan di bawah komentar utama.
	nested, err := svc.CreateComment(ctx, 9, 1, reply.ID, "balasan untuk balasan")
	require.NoError(t, err)
	assert.Equal(t, root.ID, nested.ParentID)

	assert.Equal(t, int64(2), repo.comments[root.ID].ReplyCount)
	assert.Equal(t, int64(3), repo.videos[1].commentCount)

	t.Run("komentar yang dibalas tidak ada", func(t *testing.T) {
		_, err := svc.CreateComment(ctx, 7, 1, 999, "halo")
		assert.ErrorIs(t, err, ErrNotFound)
	})
	t.Run("komentar milik video lain", func(t *testing.T) {
		_, err := svc.CreateComment(ctx, 7, 2, root.ID, "halo")
		assert.ErrorIs(t, err, ErrNotFound)
	})
}

func TestNormalizeBody(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"teks biasa", "Keren!", "Keren!", true},
		{"emoji keluarga dengan zero width joiner", "👨‍👩‍👧", "👨‍👩‍👧", true},
		{"tab dan baris baru boleh", "a\tb\nc", "a\tb\nc", true},
		{"tepat 1000 emoji", strings.Repeat("🎉", 1000), strings.Repeat("🎉", 1000), true},
		{"kosong", "", "", false},
		{"hanya spasi", " \n\t ", "", false},
		{"1001 karakter", strings.Repeat("a", 1001), "", false},
		{"karakter null", "a\x00b", "", false},
		{"pembalik arah teks", "abc‮def", "", false},
		{"bukan UTF-8", "a\xffb", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeBody(tt.in)
			if !tt.ok {
				var verr *ValidationError
				require.ErrorAs(t, err, &verr)
				assert.Equal(t, "body", verr.Field)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestListComments(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()
	var ids []int64
	for i := range 5 {
		c, err := svc.CreateComment(ctx, 7, 1, 0, "komentar "+string(rune('A'+i)))
		require.NoError(t, err)
		ids = append(ids, c.ID)
	}
	// Balasan tidak ikut di daftar komentar utama.
	_, err := svc.CreateComment(ctx, 8, 1, ids[0], "balasan")
	require.NoError(t, err)

	page1, next, err := svc.ListComments(ctx, 1, 2, "")
	require.NoError(t, err)
	assert.Equal(t, []int64{ids[4], ids[3]}, commentIDs(page1), "terbaru lebih dulu")
	require.NotEmpty(t, next)

	page2, next, err := svc.ListComments(ctx, 1, 2, next)
	require.NoError(t, err)
	assert.Equal(t, []int64{ids[2], ids[1]}, commentIDs(page2))

	page3, next, err := svc.ListComments(ctx, 1, 2, next)
	require.NoError(t, err)
	assert.Equal(t, []int64{ids[0]}, commentIDs(page3))
	assert.Equal(t, int64(1), page3[0].ReplyCount)
	assert.Empty(t, next, "halaman terakhir")

	t.Run("token rusak", func(t *testing.T) {
		for _, token := range []string{"!!!", encodeCursor(0)[:1], "LTE"} { // "LTE" = base64 dari "-1"
			_, _, err := svc.ListComments(ctx, 1, 2, token)
			var verr *ValidationError
			assert.ErrorAs(t, err, &verr, token)
		}
	})
	t.Run("page_size terlalu besar", func(t *testing.T) {
		_, _, err := svc.ListComments(ctx, 1, 51, "")
		var verr *ValidationError
		assert.ErrorAs(t, err, &verr)
	})
	t.Run("video belum READY", func(t *testing.T) {
		_, _, err := svc.ListComments(ctx, 3, 0, "")
		assert.ErrorIs(t, err, ErrVideoNotFound)
	})
}

func TestListReplies(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()
	root, err := svc.CreateComment(ctx, 7, 1, 0, "utama")
	require.NoError(t, err)
	var ids []int64
	for range 3 {
		r, err := svc.CreateComment(ctx, 8, 1, root.ID, "balasan")
		require.NoError(t, err)
		ids = append(ids, r.ID)
	}

	page1, next, err := svc.ListReplies(ctx, 1, root.ID, 2, "")
	require.NoError(t, err)
	assert.Equal(t, ids[:2], commentIDs(page1), "terlama lebih dulu")
	page2, next, err := svc.ListReplies(ctx, 1, root.ID, 2, next)
	require.NoError(t, err)
	assert.Equal(t, ids[2:], commentIDs(page2))
	assert.Empty(t, next)

	t.Run("id balasan bukan komentar utama", func(t *testing.T) {
		_, _, err := svc.ListReplies(ctx, 1, ids[0], 0, "")
		assert.ErrorIs(t, err, ErrNotFound)
	})
	t.Run("video yang salah", func(t *testing.T) {
		_, _, err := svc.ListReplies(ctx, 2, root.ID, 0, "")
		assert.ErrorIs(t, err, ErrNotFound)
	})
}

func TestDeleteComment(t *testing.T) {
	ctx := context.Background()

	t.Run("penulis, pemilik video, dan orang lain", func(t *testing.T) {
		svc, _ := newTestService()
		c, err := svc.CreateComment(ctx, 7, 1, 0, "halo")
		require.NoError(t, err)

		assert.ErrorIs(t, svc.DeleteComment(ctx, 8, 1, c.ID), ErrForbidden, "orang lain")
		require.NoError(t, svc.DeleteComment(ctx, 7, 1, c.ID), "penulis")
		assert.ErrorIs(t, svc.DeleteComment(ctx, 7, 1, c.ID), ErrNotFound, "sudah dihapus")

		c2, err := svc.CreateComment(ctx, 7, 1, 0, "spam")
		require.NoError(t, err)
		require.NoError(t, svc.DeleteComment(ctx, 100, 1, c2.ID), "pemilik video (moderasi)")
	})

	t.Run("hapus balasan mengurangi penghitung", func(t *testing.T) {
		svc, repo := newTestService()
		root, _ := svc.CreateComment(ctx, 7, 1, 0, "utama")
		reply, _ := svc.CreateComment(ctx, 8, 1, root.ID, "balasan")
		require.NoError(t, svc.DeleteComment(ctx, 8, 1, reply.ID))
		assert.Equal(t, int64(0), repo.comments[root.ID].ReplyCount)
		assert.Equal(t, int64(1), repo.videos[1].commentCount)
	})

	t.Run("hapus komentar utama ikut menghapus balasan", func(t *testing.T) {
		svc, repo := newTestService()
		root, _ := svc.CreateComment(ctx, 7, 1, 0, "utama")
		reply, _ := svc.CreateComment(ctx, 8, 1, root.ID, "balasan 1")
		_, _ = svc.CreateComment(ctx, 9, 1, root.ID, "balasan 2")
		require.Equal(t, int64(3), repo.videos[1].commentCount)

		require.NoError(t, svc.DeleteComment(ctx, 7, 1, root.ID))
		assert.Equal(t, int64(0), repo.videos[1].commentCount)
		assert.ErrorIs(t, svc.DeleteComment(ctx, 8, 1, reply.ID), ErrNotFound)
		_, err := svc.CreateComment(ctx, 8, 1, reply.ID, "terlambat")
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("id dari video lain", func(t *testing.T) {
		svc, _ := newTestService()
		c, _ := svc.CreateComment(ctx, 7, 1, 0, "halo")
		assert.ErrorIs(t, svc.DeleteComment(ctx, 7, 2, c.ID), ErrNotFound)
	})
}

func TestGRPCStatusCodes(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()
	srv := NewGRPCServer(svc, slog.New(slog.NewTextHandler(io.Discard, nil)))

	resp, err := srv.CreateComment(ctx, &commentv1.CreateCommentRequest{RequesterId: 7, VideoId: 1, Body: "halo"})
	require.NoError(t, err)
	assert.Equal(t, "halo", resp.GetComment().GetBody())
	assert.Zero(t, resp.GetComment().GetParentId())

	tests := []struct {
		name string
		call func() error
		want codes.Code
	}{
		{"isi kosong", func() error {
			_, err := srv.CreateComment(ctx, &commentv1.CreateCommentRequest{RequesterId: 7, VideoId: 1})
			return err
		}, codes.InvalidArgument},
		{"video tidak ada", func() error {
			_, err := srv.ListComments(ctx, &commentv1.ListCommentsRequest{VideoId: 99})
			return err
		}, codes.NotFound},
		{"bukan penulis", func() error {
			_, err := srv.DeleteComment(ctx, &commentv1.DeleteCommentRequest{RequesterId: 8, VideoId: 1, CommentId: resp.GetComment().GetId()})
			return err
		}, codes.PermissionDenied},
		{"requester kosong", func() error {
			_, err := srv.DeleteComment(ctx, &commentv1.DeleteCommentRequest{VideoId: 1, CommentId: 1})
			return err
		}, codes.Internal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, status.Code(tt.call()))
		})
	}
}

func commentIDs(comments []Comment) []int64 {
	ids := make([]int64, len(comments))
	for i, c := range comments {
		ids[i] = c.ID
	}
	return ids
}
