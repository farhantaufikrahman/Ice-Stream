package httpserver

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	userv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/user/v1"
	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

var (
	testPrevStart = time.Date(2026, 10, 28, 17, 0, 0, 0, time.UTC)
	testCurStart  = time.Date(2026, 11, 4, 17, 0, 0, 0, time.UTC)
	testEnd       = time.Date(2026, 11, 11, 17, 0, 0, 0, time.UTC)
)

func (f *fakeVideoClient) GetChannelStats(_ context.Context, in *videov1.GetChannelStatsRequest, _ ...grpc.CallOption) (*videov1.GetChannelStatsResponse, error) {
	f.lastStats = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.GetChannelStatsResponse{
		Daily:         []*videov1.DailyViews{{Date: "2026-11-10", Views: 3}, {Date: "2026-11-11", Views: 0}},
		Current:       &videov1.ChannelTotals{Views: 3, Danmaku: 2, Comments: 1},
		Previous:      &videov1.ChannelTotals{Views: 9},
		PreviousStart: timestamppb.New(testPrevStart),
		CurrentStart:  timestamppb.New(testCurStart),
		End:           timestamppb.New(testEnd),
	}, nil
}

func (f *fakeUserClient) CountNewSubscribers(_ context.Context, in *userv1.CountNewSubscribersRequest, _ ...grpc.CallOption) (*userv1.CountNewSubscribersResponse, error) {
	f.lastNewSubs = in
	if f.newSubsErr != nil {
		return nil, f.newSubsErr
	}
	return &userv1.CountNewSubscribersResponse{Current: 4, Previous: 1}, nil
}

func TestChannelStats(t *testing.T) {
	verifier, as := newTokenSigner(t)
	users, fake := &fakeUserClient{}, &fakeVideoClient{}
	r := newTestRouter(t, Deps{Users: users, Videos: fake, Verifier: verifier})

	assert.Equal(t, http.StatusUnauthorized, do(r, http.MethodGet, "/api/v1/me/channel-stats", "").Code)

	rec := do(r, http.MethodGet, "/api/v1/me/channel-stats", "", as(7))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, int64(7), fake.lastStats.GetRequesterId())
	assert.Equal(t, int32(28), fake.lastStats.GetDays(), "default 28 hari")
	assert.JSONEq(t, `{
		"days": 28,
		"daily": [{"date":"2026-11-10","views":3},{"date":"2026-11-11","views":0}],
		"current": {"views":3,"danmaku":2,"comments":1,"new_subscribers":4},
		"previous": {"views":9,"danmaku":0,"comments":0,"new_subscribers":1}
	}`, rec.Body.String())
	// Periode subscriber sama persis dengan periode dari VideoService.
	assert.Equal(t, int64(7), users.lastNewSubs.GetChannelId())
	assert.True(t, testPrevStart.Equal(users.lastNewSubs.GetPreviousStart().AsTime()))
	assert.True(t, testCurStart.Equal(users.lastNewSubs.GetCurrentStart().AsTime()))
	assert.True(t, testEnd.Equal(users.lastNewSubs.GetEnd().AsTime()))

	rec = do(r, http.MethodGet, "/api/v1/me/channel-stats?days=7", "", as(7))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, int32(7), fake.lastStats.GetDays())

	t.Run("days bukan angka", func(t *testing.T) {
		assert.Equal(t, http.StatusBadRequest, do(r, http.MethodGet, "/api/v1/me/channel-stats?days=abc", "", as(7)).Code)
	})
	t.Run("days ditolak VideoService", func(t *testing.T) {
		fake.err = status.Error(codes.InvalidArgument, "days: harus 7, 28, atau 90")
		t.Cleanup(func() { fake.err = nil })
		rec := do(r, http.MethodGet, "/api/v1/me/channel-stats?days=30", "", as(7))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "harus 7, 28, atau 90")
	})
	t.Run("UserService gagal: subscriber null, sisanya tetap ada", func(t *testing.T) {
		users.newSubsErr = status.Error(codes.Unavailable, "mati")
		t.Cleanup(func() { users.newSubsErr = nil })
		rec := do(r, http.MethodGet, "/api/v1/me/channel-stats", "", as(7))
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), `"current":{"views":3,"danmaku":2,"comments":1,"new_subscribers":null}`)
	})
}
