package httpserver

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	userv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/user/v1"
	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

// defaultStatsDays sama dengan pilihan awal di desain Studio ("28 hari").
const defaultStatsDays = 28

type dailyViewsResponse struct {
	// Tanggal WIB, misalnya "2026-11-11".
	Date  string `json:"date"`
	Views int64  `json:"views"`
}

type channelTotalsResponse struct {
	Views    int64 `json:"views"`
	Danmaku  int64 `json:"danmaku"`
	Comments int64 `json:"comments"`
	// null jika UserService sedang tidak bisa dihubungi. Angka lain tetap
	// dikirim supaya dasbor tidak kosong seluruhnya.
	NewSubscribers *int64 `json:"new_subscribers"`
}

type channelStatsResponse struct {
	Days int `json:"days"`
	// Tepat sebanyak days, dari yang terlama sampai hari ini.
	Daily    []dailyViewsResponse  `json:"daily"`
	Current  channelTotalsResponse `json:"current"`
	Previous channelTotalsResponse `json:"previous"`
}

// channelStats: GET /api/v1/me/channel-stats?days=28. Wajib login.
// days boleh 7, 28, atau 90 (VideoService yang memeriksanya).
//
// Tayangan, danmaku, dan komentar dari VideoService; subscriber baru dari
// UserService, dengan batas periode yang dikirim VideoService supaya semua
// angka memakai rentang waktu yang sama.
func (h *videoHandler) channelStats(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	days := defaultStatsDays
	if raw := c.Query("days"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			abortError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "days harus berupa angka")
			return
		}
		days = n
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.GetChannelStats(ctx, &videov1.GetChannelStatsRequest{RequesterId: userID, Days: int32(days)})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	body := channelStatsResponse{
		Days:     days,
		Daily:    make([]dailyViewsResponse, len(resp.GetDaily())),
		Current:  toTotalsResponse(resp.GetCurrent()),
		Previous: toTotalsResponse(resp.GetPrevious()),
	}
	for i, d := range resp.GetDaily() {
		body.Daily[i] = dailyViewsResponse{Date: d.GetDate(), Views: d.GetViews()}
	}
	if subs := h.countNewSubscribers(c.Request.Context(), userID, resp); subs != nil {
		body.Current.NewSubscribers = &subs.Current
		body.Previous.NewSubscribers = &subs.Previous
	}
	c.JSON(http.StatusOK, body)
}

// countNewSubscribers mengembalikan nil jika UserService gagal (dicatat di
// log), sama seperti info pemilik video di lookupProfiles.
func (h *videoHandler) countNewSubscribers(ctx context.Context, userID int64, st *videov1.GetChannelStatsResponse) *userv1.CountNewSubscribersResponse {
	if h.users == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, ownerLookupTimeout)
	defer cancel()
	resp, err := h.users.CountNewSubscribers(ctx, &userv1.CountNewSubscribersRequest{
		ChannelId:     userID,
		PreviousStart: st.GetPreviousStart(),
		CurrentStart:  st.GetCurrentStart(),
		End:           st.GetEnd(),
	})
	if err != nil {
		h.logger.WarnContext(ctx, "gagal menghitung subscriber baru", slog.Any("error", err))
		return nil
	}
	return resp
}

func toTotalsResponse(t *videov1.ChannelTotals) channelTotalsResponse {
	return channelTotalsResponse{Views: t.GetViews(), Danmaku: t.GetDanmaku(), Comments: t.GetComments()}
}
