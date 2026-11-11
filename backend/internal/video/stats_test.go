package video

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeActivity adalah satu danmaku (kind "D") atau komentar (kind "C").
type fakeActivity struct {
	videoID int64
	kind    string
	at      time.Time
	deleted bool
}

func (r *fakeRepo) ownerOf(videoID int64) int64 {
	if v, ok := r.videos[videoID]; ok {
		return v.OwnerID
	}
	// Video yang sudah dihapus tetap milik pemiliknya.
	return r.trash[videoID].OwnerID
}

func (r *fakeRepo) ChannelDailyViews(_ context.Context, ownerID int64, from, to string) (map[string]int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]int64{}
	for videoID, days := range r.daily {
		if r.ownerOf(videoID) != ownerID {
			continue
		}
		for day, n := range days {
			// "2006-01-02" bisa dibandingkan sebagai teks.
			if day >= from && day <= to {
				out[day] += n
			}
		}
	}
	return out, nil
}

func (r *fakeRepo) ChannelActivity(_ context.Context, ownerID int64, previousStart, currentStart, end time.Time) (ChannelTotals, ChannelTotals, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var current, previous ChannelTotals
	for _, a := range r.activity {
		if r.ownerOf(a.videoID) != ownerID || a.deleted || a.at.Before(previousStart) || !a.at.Before(end) {
			continue
		}
		t := &previous
		if !a.at.Before(currentStart) {
			t = &current
		}
		if a.kind == "D" {
			t.Danmaku++
		} else {
			t.Comments++
		}
	}
	return current, previous, nil
}

func TestStatsDay(t *testing.T) {
	// 17:00 UTC = 00:00 WIB hari berikutnya.
	assert.Equal(t, "2026-11-10", statsDay(time.Date(2026, 11, 10, 16, 59, 59, 0, time.UTC)))
	assert.Equal(t, "2026-11-11", statsDay(time.Date(2026, 11, 10, 17, 0, 0, 0, time.UTC)))
}

func TestStatsWindow(t *testing.T) {
	// 11 Nov 2026 pukul 08:00 WIB.
	prev, cur, end := statsWindow(time.Date(2026, 11, 11, 1, 0, 0, 0, time.UTC), 7)
	// end = 12 Nov 00:00 WIB = 11 Nov 17:00 UTC.
	assert.Equal(t, time.Date(2026, 11, 11, 17, 0, 0, 0, time.UTC), end)
	// Periode ini 5 sampai 11 Nov, periode sebelumnya 29 Okt sampai 4 Nov.
	assert.Equal(t, time.Date(2026, 11, 4, 17, 0, 0, 0, time.UTC), cur)
	assert.Equal(t, time.Date(2026, 10, 28, 17, 0, 0, 0, time.UTC), prev)
}

func TestRecordViewCountsPerDay(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	v := readyVideo(repo, 7, time.Now())
	// 23:59:58 WIB lalu 00:00:00 WIB keesokan harinya (clockAt menambah
	// satu detik setiap dipanggil).
	svc.now = clockAt(time.Date(2026, 11, 10, 16, 59, 57, 0, time.UTC))

	_, err := svc.RecordView(ctx, v.ID, "u-1")
	require.NoError(t, err)
	_, err = svc.RecordView(ctx, v.ID, "u-2")
	require.NoError(t, err)
	_, err = svc.RecordView(ctx, v.ID, "u-3")
	require.NoError(t, err)

	assert.Equal(t, map[string]int64{"2026-11-10": 2, "2026-11-11": 1}, repo.daily[v.ID])
}

func TestGetChannelStats(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	// 11 Nov 2026 pukul 10:00 WIB. Periode 7 hari = 5 sampai 11 Nov,
	// sebelumnya = 29 Okt sampai 4 Nov.
	svc.now = func() time.Time { return time.Date(2026, 11, 11, 3, 0, 0, 0, time.UTC) }
	mine := readyVideo(repo, 7, time.Now())
	other := readyVideo(repo, 8, time.Now())
	gone := readyVideo(repo, 7, time.Now())
	// Video yang sudah dihapus tetap dihitung.
	repo.trash[gone.ID] = repo.videos[gone.ID]
	delete(repo.videos, gone.ID)

	repo.daily = map[int64]map[string]int64{
		mine.ID:  {"2026-10-28": 100, "2026-10-29": 4, "2026-11-04": 6, "2026-11-05": 3, "2026-11-11": 5},
		gone.ID:  {"2026-11-05": 2},
		other.ID: {"2026-11-11": 50},
	}
	wib := func(day, hour int) time.Time { return time.Date(2026, 11, day, hour, 0, 0, 0, statsZone) }
	repo.activity = []fakeActivity{
		{videoID: mine.ID, kind: "D", at: wib(11, 9)},
		{videoID: mine.ID, kind: "D", at: wib(5, 0)},
		{videoID: gone.ID, kind: "D", at: wib(4, 23)},                                      // periode sebelumnya
		{videoID: mine.ID, kind: "C", at: wib(6, 12)},                                      // komentar
		{videoID: mine.ID, kind: "C", at: wib(6, 13), deleted: true},                       // dihapus: tidak dihitung
		{videoID: mine.ID, kind: "C", at: time.Date(2026, 10, 28, 23, 0, 0, 0, statsZone)}, // di luar kedua periode
		{videoID: other.ID, kind: "D", at: wib(11, 8)},                                     // channel lain
	}

	st, err := svc.GetChannelStats(ctx, 7, 7)
	require.NoError(t, err)

	require.Len(t, st.Daily, 7)
	assert.Equal(t, DailyViews{Date: "2026-11-05", Views: 5}, st.Daily[0])
	assert.Equal(t, DailyViews{Date: "2026-11-06", Views: 0}, st.Daily[1], "hari tanpa tayangan tetap ada")
	assert.Equal(t, DailyViews{Date: "2026-11-11", Views: 5}, st.Daily[6])
	assert.Equal(t, ChannelTotals{Views: 10, Danmaku: 2, Comments: 1}, st.Current)
	// 28 Okt di luar periode sebelumnya (29 Okt sampai 4 Nov).
	assert.Equal(t, ChannelTotals{Views: 10, Danmaku: 1}, st.Previous)
	assert.Equal(t, time.Date(2026, 11, 11, 17, 0, 0, 0, time.UTC), st.End)
}

func TestGetChannelStatsValidation(t *testing.T) {
	svc, _, _ := newTestService()
	var verr *ValidationError
	for _, days := range []int{0, 1, 30, 365} {
		_, err := svc.GetChannelStats(context.Background(), 7, days)
		assert.ErrorAs(t, err, &verr, "days=%d", days)
	}
	_, err := svc.GetChannelStats(context.Background(), 0, 7)
	assert.ErrorIs(t, err, ErrNotFound)

	// 90 hari menghasilkan 90 titik.
	st, err := svc.GetChannelStats(context.Background(), 7, 90)
	require.NoError(t, err)
	assert.Len(t, st.Daily, 90)
}
