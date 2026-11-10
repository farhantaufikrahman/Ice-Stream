package httpserver

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	commentv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/comment/v1"
	userv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/user/v1"
)

// commentHandler meneruskan request komentar ke CommentService (gRPC), lalu
// melengkapi setiap komentar dengan nama dan foto penulisnya dari
// UserService (API composition, sama seperti owner di feed video).
type commentHandler struct {
	client commentv1.CommentServiceClient
	// users dipakai untuk mengambil info penulis. Boleh nil (di test).
	users  userv1.UserServiceClient
	logger *slog.Logger
}

type createCommentRequest struct {
	Body string `json:"body"`
	// ParentID diisi id komentar yang dibalas, sebagai string seperti id
	// lain di API ini. Kosong atau tidak dikirim = komentar utama.
	ParentID string `json:"parent_id"`
}

type commentResponse struct {
	ID      string `json:"id"`
	VideoID string `json:"video_id"`
	// ParentID hanya ada untuk balasan: id komentar utamanya.
	ParentID string `json:"parent_id,omitempty"`
	AuthorID string `json:"author_id"`
	// Author berisi nama dan foto penulis. Tidak ada jika UserService
	// sedang mati, atau akun penulisnya diblokir.
	Author     *ownerResponse `json:"author,omitempty"`
	Body       string         `json:"body"`
	ReplyCount int64          `json:"reply_count"`
	CreatedAt  time.Time      `json:"created_at"`
}

type listCommentsResponse struct {
	Comments []commentResponse `json:"comments"`
	// Kosong jika sudah halaman terakhir. Kirim kembali sebagai
	// ?page_token=... untuk mengambil halaman berikutnya.
	NextPageToken string `json:"next_page_token"`
}

// list: GET /api/v1/videos/:id/comments?page_size=&page_token=
// Komentar utama, terbaru lebih dulu. Tanpa login.
func (h *commentHandler) list(c *gin.Context) {
	videoID, ok := videoIDParam(c)
	if !ok {
		return
	}
	pageSize, ok := pageSizeQuery(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.ListComments(ctx, &commentv1.ListCommentsRequest{
		VideoId:   videoID,
		PageSize:  pageSize,
		PageToken: c.Query("page_token"),
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, h.toListResponse(c.Request.Context(), resp.GetComments(), resp.GetNextPageToken()))
}

// listReplies: GET /api/v1/videos/:id/comments/:commentId/replies
// Balasan untuk satu komentar utama, terlama lebih dulu. Tanpa login.
func (h *commentHandler) listReplies(c *gin.Context) {
	videoID, ok := videoIDParam(c)
	if !ok {
		return
	}
	commentID, ok := commentIDParam(c)
	if !ok {
		return
	}
	pageSize, ok := pageSizeQuery(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.ListReplies(ctx, &commentv1.ListRepliesRequest{
		VideoId:   videoID,
		CommentId: commentID,
		PageSize:  pageSize,
		PageToken: c.Query("page_token"),
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, h.toListResponse(c.Request.Context(), resp.GetComments(), resp.GetNextPageToken()))
}

// create: POST /api/v1/videos/:id/comments {"body": "...", "parent_id": "12"}
// Wajib login. Penulis diambil dari access token, bukan dari body.
func (h *commentHandler) create(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	videoID, ok := videoIDParam(c)
	if !ok {
		return
	}
	var req createCommentRequest
	if !bindJSON(c, &req) {
		return
	}
	var parentID int64
	if req.ParentID != "" {
		id, err := strconv.ParseInt(req.ParentID, 10, 64)
		if err != nil || id <= 0 {
			abortError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "parent_id tidak valid")
			return
		}
		parentID = id
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.CreateComment(ctx, &commentv1.CreateCommentRequest{
		RequesterId: userID,
		VideoId:     videoID,
		ParentId:    parentID,
		Body:        req.Body,
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	body := h.toListResponse(c.Request.Context(), []*commentv1.Comment{resp.GetComment()}, "")
	c.JSON(http.StatusCreated, gin.H{"comment": body.Comments[0]})
}

// remove: DELETE /api/v1/videos/:id/comments/:commentId
// Wajib login, dan hanya penulis atau pemilik video. Berhasil = 204 tanpa body.
func (h *commentHandler) remove(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	videoID, ok := videoIDParam(c)
	if !ok {
		return
	}
	commentID, ok := commentIDParam(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	_, err := h.client.DeleteComment(ctx, &commentv1.DeleteCommentRequest{
		RequesterId: userID,
		VideoId:     videoID,
		CommentId:   commentID,
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// toListResponse mengubah komentar menjadi JSON dan mengisi info penulis
// dengan satu panggilan BatchGetProfiles untuk seluruh halaman.
func (h *commentHandler) toListResponse(ctx context.Context, comments []*commentv1.Comment, next string) listCommentsResponse {
	ids := make([]int64, 0, len(comments))
	for _, cm := range comments {
		ids = append(ids, cm.GetAuthorId())
	}
	authors := lookupProfiles(ctx, h.users, h.logger, ids)

	out := listCommentsResponse{Comments: make([]commentResponse, 0, len(comments)), NextPageToken: next}
	for _, cm := range comments {
		r := commentResponse{
			ID:         strconv.FormatInt(cm.GetId(), 10),
			VideoID:    strconv.FormatInt(cm.GetVideoId(), 10),
			AuthorID:   strconv.FormatInt(cm.GetAuthorId(), 10),
			Author:     authors[cm.GetAuthorId()],
			Body:       cm.GetBody(),
			ReplyCount: cm.GetReplyCount(),
			CreatedAt:  cm.GetCreatedAt().AsTime(),
		}
		if cm.GetParentId() != 0 {
			r.ParentID = strconv.FormatInt(cm.GetParentId(), 10)
		}
		out.Comments = append(out.Comments, r)
	}
	return out
}

// commentIDParam membaca :commentId dari URL. Id yang bukan angka dijawab
// 404, sama seperti id yang tidak ada.
func commentIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("commentId"), 10, 64)
	if err != nil || id <= 0 {
		abortError(c, http.StatusNotFound, "NOT_FOUND", "komentar tidak ditemukan")
		return 0, false
	}
	return id, true
}
