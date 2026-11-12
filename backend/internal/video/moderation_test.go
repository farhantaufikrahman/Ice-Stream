package video

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeModeration adalah baris video_moderation palsu.
type fakeModeration struct {
	m        Moderation
	restored bool
}

func (r *fakeRepo) openReports(videoID int64) []Report {
	var out []Report
	for key, rep := range r.reports {
		if key[0] != videoID {
			continue
		}
		if _, done := r.resolved[key]; !done {
			out = append(out, rep)
		}
	}
	slices.SortFunc(out, func(a, b Report) int {
		if c := b.ReportedAt.Compare(a.ReportedAt); c != 0 {
			return c
		}
		return cmp.Compare(a.ReporterID, b.ReporterID)
	})
	return out
}

func (r *fakeRepo) ListReportedVideos(_ context.Context, offset, limit int) ([]ReportedVideo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var all []ReportedVideo
	seen := map[int64]bool{}
	for key := range r.reports {
		id := key[0]
		v, ok := r.videos[id]
		if seen[id] || !ok {
			continue
		}
		seen[id] = true
		open := r.openReports(id)
		if len(open) == 0 {
			continue
		}
		counts := map[ReportReason]int64{}
		item := ReportedVideo{Video: v, OpenReports: int64(len(open)), FirstReportedAt: open[len(open)-1].ReportedAt, LastReportedAt: open[0].ReportedAt}
		for _, rep := range open {
			counts[rep.Reason]++
		}
		for reason, n := range counts {
			item.Reasons = append(item.Reasons, ReasonCount{Reason: reason, Count: n})
		}
		slices.SortFunc(item.Reasons, func(a, b ReasonCount) int {
			if c := cmp.Compare(b.Count, a.Count); c != 0 {
				return c
			}
			return cmp.Compare(a.Reason, b.Reason)
		})
		all = append(all, item)
	}
	slices.SortFunc(all, func(a, b ReportedVideo) int {
		if c := cmp.Compare(b.OpenReports, a.OpenReports); c != 0 {
			return c
		}
		if c := a.FirstReportedAt.Compare(b.FirstReportedAt); c != 0 {
			return c
		}
		return cmp.Compare(a.Video.ID, b.Video.ID)
	})
	if offset >= len(all) {
		return nil, nil
	}
	all = all[offset:]
	return all[:min(limit, len(all))], nil
}

func (r *fakeRepo) ListVideoReports(_ context.Context, videoID int64, limit int) ([]Report, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	open := r.openReports(videoID)
	return open[:min(limit, len(open))], nil
}

func (r *fakeRepo) resolveLocked(videoID, moderatorID int64, resolution string) int64 {
	if r.resolved == nil {
		r.resolved = map[[2]int64]string{}
	}
	var n int64
	for _, rep := range r.openReports(videoID) {
		r.resolved[[2]int64{videoID, rep.ReporterID}] = resolution
		n++
	}
	return n
}

func (r *fakeRepo) notifyOwnerLocked(ownerID, videoID, actorID int64, kind string, now time.Time) {
	r.notifs = slices.DeleteFunc(r.notifs, func(n fakeNotif) bool {
		return n.userID == ownerID && n.videoID == videoID && n.kind == kind
	})
	r.notifs = append(r.notifs, fakeNotif{id: int64(len(r.notifs) + 100), userID: ownerID, videoID: videoID, actorID: actorID, kind: kind, createdAt: now})
}

func (r *fakeRepo) BlockVideo(_ context.Context, in BlockInput, now time.Time) (BlockedVideo, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.videos[in.VideoID]
	if !ok {
		return BlockedVideo{}, 0, ErrNotFound
	}
	if v.Visibility == VisibilityBlocked {
		return BlockedVideo{}, 0, ErrAlreadyBlocked
	}
	m := Moderation{VideoID: v.ID, PreviousVisibility: v.Visibility, Reason: in.Reason, Note: in.Note, ModeratorID: in.ModeratorID, BlockedAt: now}
	if r.moderation == nil {
		r.moderation = map[int64]fakeModeration{}
	}
	r.moderation[v.ID] = fakeModeration{m: m}
	v.Visibility = VisibilityBlocked
	r.videos[v.ID] = v
	closed := r.resolveLocked(v.ID, in.ModeratorID, ResolutionBlocked)
	r.notifyOwnerLocked(v.OwnerID, v.ID, in.ModeratorID, NotificationVideoBlocked, now)
	v.Moderation = &m
	return BlockedVideo{Video: v, Moderation: m}, closed, nil
}

func (r *fakeRepo) ResolveReports(_ context.Context, videoID, moderatorID int64, resolution string, _ time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.videos[videoID]; !ok {
		return 0, ErrNotFound
	}
	return r.resolveLocked(videoID, moderatorID, resolution), nil
}

func (r *fakeRepo) UnblockVideo(_ context.Context, videoID, moderatorID int64, now time.Time) (Video, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.videos[videoID]
	if !ok {
		return Video{}, ErrNotFound
	}
	if v.Visibility != VisibilityBlocked {
		return Video{}, ErrNotBlocked
	}
	fm := r.moderation[videoID]
	v.Visibility = fm.m.PreviousVisibility
	r.videos[videoID] = v
	fm.restored = true
	r.moderation[videoID] = fm
	r.notifyOwnerLocked(v.OwnerID, videoID, moderatorID, NotificationVideoRestored, now)
	return v, nil
}

func (r *fakeRepo) ListBlockedVideos(_ context.Context, offset, limit int) ([]BlockedVideo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var all []BlockedVideo
	for id, fm := range r.moderation {
		v, ok := r.videos[id]
		if fm.restored || !ok || v.Visibility != VisibilityBlocked {
			continue
		}
		m := fm.m
		v.Moderation = &m
		all = append(all, BlockedVideo{Video: v, Moderation: m})
	}
	slices.SortFunc(all, func(a, b BlockedVideo) int {
		if c := b.Moderation.BlockedAt.Compare(a.Moderation.BlockedAt); c != 0 {
			return c
		}
		return cmp.Compare(b.Video.ID, a.Video.ID)
	})
	if offset >= len(all) {
		return nil, nil
	}
	all = all[offset:]
	return all[:min(limit, len(all))], nil
}

func (r *fakeRepo) FindModerations(_ context.Context, ids []int64) (map[int64]Moderation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[int64]Moderation{}
	for _, id := range ids {
		if fm, ok := r.moderation[id]; ok && !fm.restored {
			out[id] = fm.m
		}
	}
	return out, nil
}

func TestModerationQueueAndBlock(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	now := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	a := readyVideo(repo, 7, now)
	b := readyVideo(repo, 8, now)
	// Video b dilaporkan lebih banyak, jadi tampil pertama.
	for i, reporter := range []int64{20, 21} {
		svc.now = func() time.Time { return now.Add(time.Duration(i) * time.Minute) }
		_, err := svc.ReportVideo(ctx, reporter, b.ID, ReportSpam, "")
		require.NoError(t, err)
	}
	_, err := svc.ReportVideo(ctx, 22, b.ID, ReportHate, "")
	require.NoError(t, err)
	_, err = svc.ReportVideo(ctx, 20, a.ID, ReportCopyright, "lagu saya")
	require.NoError(t, err)
	svc.now = func() time.Time { return now.Add(time.Hour) }

	queue, next, err := svc.ListReportedVideos(ctx, 1, "")
	require.NoError(t, err)
	require.Len(t, queue, 1)
	assert.Equal(t, "1", next)
	assert.Equal(t, b.ID, queue[0].Video.ID)
	assert.Equal(t, int64(3), queue[0].OpenReports)
	assert.Equal(t, []ReasonCount{{ReportSpam, 2}, {ReportHate, 1}}, queue[0].Reasons)

	queue, next, err = svc.ListReportedVideos(ctx, 1, next)
	require.NoError(t, err)
	require.Len(t, queue, 1)
	assert.Empty(t, next)
	assert.Equal(t, a.ID, queue[0].Video.ID)

	reports, err := svc.ListVideoReports(ctx, a.ID)
	require.NoError(t, err)
	require.Len(t, reports, 1)
	assert.Equal(t, "lagu saya", reports[0].Details)

	// Turunkan b: semua laporan b ditutup, video hilang dari daftar publik,
	// pemilik mendapat notifikasi, dan pemilik melihat alasannya.
	blocked, closed, err := svc.BlockVideo(ctx, BlockInput{ModeratorID: 1, VideoID: b.ID, Reason: ReportSpam, Note: "  link judi  "})
	require.NoError(t, err)
	assert.Equal(t, int64(3), closed)
	assert.Equal(t, VisibilityBlocked, blocked.Video.Visibility)
	assert.Equal(t, "link judi", blocked.Moderation.Note)
	assert.False(t, repo.videos[b.ID].IsListed())
	assert.False(t, repo.videos[b.ID].IsWatchable())

	queue, _, err = svc.ListReportedVideos(ctx, 10, "")
	require.NoError(t, err)
	require.Len(t, queue, 1)
	assert.Equal(t, a.ID, queue[0].Video.ID)

	require.True(t, slices.ContainsFunc(repo.notifs, func(n fakeNotif) bool {
		return n.userID == 8 && n.videoID == b.ID && n.kind == NotificationVideoBlocked && n.actorID == 1
	}))

	// Orang lain tidak bisa membuka atau melaporkan video yang diturunkan.
	_, err = svc.GetVideo(ctx, 30, b.ID)
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = svc.ReportVideo(ctx, 30, b.ID, ReportSpam, "")
	assert.ErrorIs(t, err, ErrNotFound)

	// Pemilik melihat alasan di Studio.
	mine, _, err := svc.ListMyVideos(ctx, 8, 10, "")
	require.NoError(t, err)
	require.Len(t, mine, 1)
	require.NotNil(t, mine[0].Moderation)
	assert.Equal(t, ReportSpam, mine[0].Moderation.Reason)

	// Pemilik tidak bisa mengubah visibilitas, tapi judul tetap boleh.
	public := VisibilityPublic
	_, err = svc.UpdateVideo(ctx, UpdateInput{RequesterID: 8, VideoID: b.ID, Visibility: &public})
	var verr *ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "visibility", verr.Field)
	title := "Judul baru"
	updated, err := svc.UpdateVideo(ctx, UpdateInput{RequesterID: 8, VideoID: b.ID, Title: &title})
	require.NoError(t, err)
	assert.Equal(t, VisibilityBlocked, updated.Visibility)

	// Menurunkan dua kali ditolak.
	_, _, err = svc.BlockVideo(ctx, BlockInput{ModeratorID: 1, VideoID: b.ID, Reason: ReportSpam})
	assert.ErrorIs(t, err, ErrAlreadyBlocked)

	list, _, err := svc.ListBlockedVideos(ctx, 10, "")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, b.ID, list[0].Video.ID)

	// Pulihkan: visibilitas kembali PUBLIC, pemilik diberi tahu.
	restored, err := svc.UnblockVideo(ctx, 1, b.ID)
	require.NoError(t, err)
	assert.Equal(t, VisibilityPublic, restored.Visibility)
	assert.True(t, repo.videos[b.ID].IsListed())
	_, err = svc.UnblockVideo(ctx, 1, b.ID)
	assert.ErrorIs(t, err, ErrNotBlocked)
	list, _, err = svc.ListBlockedVideos(ctx, 10, "")
	require.NoError(t, err)
	assert.Empty(t, list)
	assert.True(t, slices.ContainsFunc(repo.notifs, func(n fakeNotif) bool {
		return n.userID == 8 && n.kind == NotificationVideoRestored
	}))
}

func TestModerationRestoresPreviousVisibility(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	v := readyVideo(repo, 7, time.Now())
	unlisted := VisibilityUnlisted
	_, err := svc.UpdateVideo(ctx, UpdateInput{RequesterID: 7, VideoID: v.ID, Visibility: &unlisted})
	require.NoError(t, err)

	_, _, err = svc.BlockVideo(ctx, BlockInput{ModeratorID: 1, VideoID: v.ID, Reason: ReportOther, Note: "melanggar"})
	require.NoError(t, err)
	got, err := svc.UnblockVideo(ctx, 1, v.ID)
	require.NoError(t, err)
	assert.Equal(t, VisibilityUnlisted, got.Visibility)
}

func TestDismissReports(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	v := readyVideo(repo, 7, time.Now())
	_, err := svc.ReportVideo(ctx, 20, v.ID, ReportMisleading, "")
	require.NoError(t, err)

	closed, err := svc.DismissReports(ctx, 1, v.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), closed)
	assert.Equal(t, ResolutionDismissed, repo.resolved[[2]int64{v.ID, 20}])
	assert.True(t, repo.videos[v.ID].IsListed())

	// Dilaporkan lagi = dibuka lagi.
	_, err = svc.ReportVideo(ctx, 20, v.ID, ReportMisleading, "")
	require.NoError(t, err)
	queue, _, err := svc.ListReportedVideos(ctx, 10, "")
	require.NoError(t, err)
	assert.Len(t, queue, 1)

	_, err = svc.DismissReports(ctx, 1, 999)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestModerationRejects(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	v := readyVideo(repo, 7, time.Now())
	var verr *ValidationError
	for _, tc := range []struct {
		name  string
		in    BlockInput
		field string
	}{
		{"alasan kosong", BlockInput{ModeratorID: 1, VideoID: v.ID}, "reason"},
		{"alasan tidak dikenal", BlockInput{ModeratorID: 1, VideoID: v.ID, Reason: "bosan"}, "reason"},
		{"lainnya tanpa catatan", BlockInput{ModeratorID: 1, VideoID: v.ID, Reason: ReportOther, Note: "  "}, "note"},
		{"catatan terlalu panjang", BlockInput{ModeratorID: 1, VideoID: v.ID, Reason: ReportSpam, Note: strings.Repeat("é", 501)}, "note"},
	} {
		_, _, err := svc.BlockVideo(ctx, tc.in)
		require.ErrorAs(t, err, &verr, tc.name)
		assert.Equal(t, tc.field, verr.Field, tc.name)
	}
	_, _, err := svc.BlockVideo(ctx, BlockInput{ModeratorID: 1, VideoID: 999, Reason: ReportSpam})
	assert.ErrorIs(t, err, ErrNotFound)
	_, _, err = svc.BlockVideo(ctx, BlockInput{VideoID: v.ID, Reason: ReportSpam})
	assert.Error(t, err)
	_, _, err = svc.ListReportedVideos(ctx, 51, "")
	require.ErrorAs(t, err, &verr)
	_, _, err = svc.ListBlockedVideos(ctx, 10, "abc")
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "page_token", verr.Field)

	// Pemilik tidak bisa memilih BLOCKED sendiri.
	blocked := VisibilityBlocked
	_, err = svc.UpdateVideo(ctx, UpdateInput{RequesterID: 7, VideoID: v.ID, Visibility: &blocked})
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "visibility", verr.Field)
}
