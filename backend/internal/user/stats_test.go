package user

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	userv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/user/v1"
)

func (f *fakeSubs) CountNewSubscribers(_ context.Context, channel int64, previousStart, currentStart, end time.Time) (int64, int64, error) {
	f.subMu.Lock()
	defer f.subMu.Unlock()
	var current, previous int64
	for _, s := range f.subs {
		at := f.createdAt[s.id]
		if s.channel != channel || at.Before(previousStart) || !at.Before(end) {
			continue
		}
		if at.Before(currentStart) {
			previous++
		} else {
			current++
		}
	}
	return current, previous, nil
}

func TestCountNewSubscribers(t *testing.T) {
	ctx := context.Background()
	svc, subs := newSubsService()
	srv := NewGRPCServer(svc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	prev := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	cur := prev.AddDate(0, 0, 7)
	end := cur.AddDate(0, 0, 7)

	subs.createdAt = map[int64]time.Time{}
	add := func(subscriber, channel int64, at time.Time) {
		subs.nextID++
		subs.subs = append(subs.subs, fakeSub{subs.nextID, subscriber, channel})
		subs.createdAt[subs.nextID] = at
	}
	add(101, 7, prev.Add(-time.Second))    // sebelum kedua periode
	add(102, 7, prev)                      // periode sebelumnya (batas awal ikut)
	add(103, 7, cur.Add(-time.Second))     // periode sebelumnya
	add(104, 7, cur)                       // periode ini
	add(105, 7, end.Add(-time.Nanosecond)) // periode ini
	add(106, 7, end)                       // setelah periode (batas akhir tidak ikut)
	add(107, 8, cur)                       // channel lain

	resp, err := srv.CountNewSubscribers(ctx, &userv1.CountNewSubscribersRequest{
		ChannelId:     7,
		PreviousStart: timestamppb.New(prev),
		CurrentStart:  timestamppb.New(cur),
		End:           timestamppb.New(end),
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), resp.GetCurrent())
	assert.Equal(t, int64(2), resp.GetPrevious())

	for name, req := range map[string]*userv1.CountNewSubscribersRequest{
		"tanpa waktu":  {ChannelId: 7},
		"urutan salah": {ChannelId: 7, PreviousStart: timestamppb.New(cur), CurrentStart: timestamppb.New(prev), End: timestamppb.New(end)},
		"periode nol":  {ChannelId: 7, PreviousStart: timestamppb.New(prev), CurrentStart: timestamppb.New(cur), End: timestamppb.New(cur)},
	} {
		_, err := srv.CountNewSubscribers(ctx, req)
		assert.Equal(t, codes.InvalidArgument, status.Code(err), name)
	}
	_, err = srv.CountNewSubscribers(ctx, &userv1.CountNewSubscribersRequest{})
	assert.Equal(t, codes.NotFound, status.Code(err))
}
