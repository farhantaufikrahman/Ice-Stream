package user

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	userv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/user/v1"
)

// fakeSubs menyimpan subscription di memori. Disimpan terpisah dari
// fakeRepo (lihat service_test.go) lewat embedding, supaya test lama
// tidak perlu diubah.
type fakeSubs struct {
	*fakeRepo
	subMu  sync.Mutex
	nextID int64
	subs   []fakeSub
}

type fakeSub struct {
	id, subscriber, channel int64
}

func (f *fakeSubs) find(subscriber, channel int64) int {
	return slices.IndexFunc(f.subs, func(s fakeSub) bool { return s.subscriber == subscriber && s.channel == channel })
}

func (f *fakeSubs) channel(id int64, activeOnly bool) (Profile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.users[id]
	if !ok || (activeOnly && p.Status == statusBanned) {
		return Profile{}, ErrNotFound
	}
	return p, nil
}

func (f *fakeSubs) addCount(id, delta int64) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.users[id]
	p.SubscriberCount += delta
	f.users[id] = p
	return p.SubscriberCount
}

func (f *fakeSubs) Subscribe(_ context.Context, subscriber, channel int64, maxSubs int) (int64, error) {
	f.subMu.Lock()
	defer f.subMu.Unlock()
	ch, err := f.channel(channel, true)
	if err != nil {
		return 0, err
	}
	if f.find(subscriber, channel) >= 0 {
		return ch.SubscriberCount, nil
	}
	total := 0
	for _, s := range f.subs {
		if s.subscriber == subscriber {
			total++
		}
	}
	if total >= maxSubs {
		return 0, ErrTooManySubscriptions
	}
	f.nextID++
	f.subs = append(f.subs, fakeSub{f.nextID, subscriber, channel})
	return f.addCount(channel, 1), nil
}

func (f *fakeSubs) Unsubscribe(_ context.Context, subscriber, channel int64) (int64, error) {
	f.subMu.Lock()
	defer f.subMu.Unlock()
	ch, err := f.channel(channel, false)
	if err != nil {
		return 0, err
	}
	i := f.find(subscriber, channel)
	if i < 0 {
		return ch.SubscriberCount, nil
	}
	f.subs = slices.Delete(f.subs, i, i+1)
	return f.addCount(channel, -1), nil
}

func (f *fakeSubs) IsSubscribed(_ context.Context, subscriber, channel int64) (bool, error) {
	f.subMu.Lock()
	defer f.subMu.Unlock()
	return f.find(subscriber, channel) >= 0, nil
}

func (f *fakeSubs) ListSubscriptions(_ context.Context, subscriber, beforeID int64, limit int) ([]Subscription, error) {
	f.subMu.Lock()
	defer f.subMu.Unlock()
	var out []Subscription
	for i := len(f.subs) - 1; i >= 0 && len(out) < limit; i-- {
		s := f.subs[i]
		if s.subscriber == subscriber && (beforeID == 0 || s.id < beforeID) {
			out = append(out, Subscription{ID: s.id, ChannelID: s.channel})
		}
	}
	return out, nil
}

func (f *fakeSubs) ListSubscribedChannelIDs(ctx context.Context, subscriber int64, limit int) ([]int64, error) {
	subs, err := f.ListSubscriptions(ctx, subscriber, 0, limit)
	ids := make([]int64, len(subs))
	for i, s := range subs {
		ids[i] = s.ChannelID
	}
	return ids, err
}

func newSubsService() (*Service, *fakeSubs) {
	_, repo, store := newTestService()
	repo.users[10] = Profile{ID: 10, Username: "tono", DisplayName: "Tono", Status: "ACTIVE"}
	subs := &fakeSubs{fakeRepo: repo}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewService(subs, store, logger, "http://media.test/images"), subs
}

func TestSubscribe(t *testing.T) {
	ctx := context.Background()
	svc, _ := newSubsService()

	r, err := svc.Subscribe(ctx, 8, 7)
	require.NoError(t, err)
	assert.Equal(t, SubscribeResult{Subscribed: true, SubscriberCount: 1}, r)

	// Subscribe kedua tidak menambah jumlah (idempotent).
	r, err = svc.Subscribe(ctx, 8, 7)
	require.NoError(t, err)
	assert.Equal(t, int64(1), r.SubscriberCount)

	r, err = svc.Subscribe(ctx, 10, 7)
	require.NoError(t, err)
	assert.Equal(t, int64(2), r.SubscriberCount)

	p, err := svc.GetProfile(ctx, 0, 7, "")
	require.NoError(t, err)
	assert.Equal(t, int64(2), p.SubscriberCount)

	for viewer, want := range map[int64]bool{8: true, 10: true, 7: false, 0: false} {
		got, err := svc.ViewerSubscribed(ctx, viewer, 7)
		require.NoError(t, err)
		assert.Equal(t, want, got, "viewer %d", viewer)
	}

	r, err = svc.Unsubscribe(ctx, 8, 7)
	require.NoError(t, err)
	assert.Equal(t, SubscribeResult{Subscribed: false, SubscriberCount: 1}, r)
	r, err = svc.Unsubscribe(ctx, 8, 7)
	require.NoError(t, err)
	assert.Equal(t, int64(1), r.SubscriberCount, "unsubscribe kedua tidak mengurangi lagi")

	t.Run("diri sendiri", func(t *testing.T) {
		_, err := svc.Subscribe(ctx, 7, 7)
		var verr *ValidationError
		assert.ErrorAs(t, err, &verr)
	})
	t.Run("channel diblokir atau tidak ada", func(t *testing.T) {
		_, err := svc.Subscribe(ctx, 8, 9)
		assert.ErrorIs(t, err, ErrNotFound)
		_, err = svc.Subscribe(ctx, 8, 999)
		assert.ErrorIs(t, err, ErrNotFound)
	})
}

func TestSubscribeLimit(t *testing.T) {
	ctx := context.Background()
	svc, subs := newSubsService()
	// Isi langsung 1000 subscription palsu milik user 8.
	for i := range maxSubscriptions {
		subs.nextID++
		subs.subs = append(subs.subs, fakeSub{subs.nextID, 8, int64(1000 + i)})
	}
	_, err := svc.Subscribe(ctx, 8, 7)
	assert.ErrorIs(t, err, ErrTooManySubscriptions)
}

func TestListSubscriptions(t *testing.T) {
	ctx := context.Background()
	svc, subs := newSubsService()
	for _, ch := range []int64{7, 10, 9} {
		subs.nextID++
		subs.subs = append(subs.subs, fakeSub{subs.nextID, 8, ch})
	}

	// Terbaru lebih dulu: 9 (diblokir, dilewati), 10, lalu 7.
	page1, next, err := svc.ListSubscriptions(ctx, 8, 2, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"tono"}, usernames(page1), "channel diblokir dilewati")
	require.NotEmpty(t, next)
	for _, p := range page1 {
		assert.Empty(t, p.Email, "tanpa email")
	}

	page2, next, err := svc.ListSubscriptions(ctx, 8, 2, next)
	require.NoError(t, err)
	assert.Equal(t, []string{"budi"}, usernames(page2))
	assert.Empty(t, next)

	ids, err := svc.ListSubscribedChannelIDs(ctx, 8)
	require.NoError(t, err)
	assert.Equal(t, []int64{9, 10, 7}, ids)

	_, _, err = svc.ListSubscriptions(ctx, 8, 0, "!!")
	var verr *ValidationError
	assert.ErrorAs(t, err, &verr)
	_, _, err = svc.ListSubscriptions(ctx, 8, 51, "")
	assert.ErrorAs(t, err, &verr)
}

func TestSubscriptionGRPC(t *testing.T) {
	ctx := context.Background()
	svc, subs := newSubsService()
	srv := NewGRPCServer(svc, slog.New(slog.NewTextHandler(io.Discard, nil)))

	resp, err := srv.Subscribe(ctx, &userv1.SubscribeRequest{RequesterId: 8, ChannelId: 7})
	require.NoError(t, err)
	assert.True(t, resp.GetSubscribed())

	got, err := srv.GetProfile(ctx, &userv1.GetProfileRequest{User: &userv1.GetProfileRequest_Username{Username: "budi"}, ViewerId: 8})
	require.NoError(t, err)
	assert.True(t, got.GetViewerSubscribed())
	assert.Equal(t, int64(1), got.GetProfile().GetSubscriberCount())

	for i := range maxSubscriptions {
		subs.nextID++
		subs.subs = append(subs.subs, fakeSub{subs.nextID, 10, int64(2000 + i)})
	}
	_, err = srv.Subscribe(ctx, &userv1.SubscribeRequest{RequesterId: 10, ChannelId: 7})
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	_, err = srv.Subscribe(ctx, &userv1.SubscribeRequest{RequesterId: 7, ChannelId: 7})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func usernames(ps []Profile) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Username
	}
	return out
}
