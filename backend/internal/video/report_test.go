package video

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SaveReport palsu: meniru MERGE di report_repository.go (satu laporan per
// pasangan video dan pelapor).
func (r *fakeRepo) SaveReport(_ context.Context, rep Report) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.reports == nil {
		r.reports = map[[2]int64]Report{}
	}
	r.reports[[2]int64{rep.VideoID, rep.ReporterID}] = rep
	return nil
}

func TestReportVideo(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	now := time.Date(2026, 11, 11, 10, 0, 0, 123456789, time.UTC)
	svc.now = func() time.Time { return now }
	v := readyVideo(repo, 7, now)

	at, err := svc.ReportVideo(ctx, 9, v.ID, ReportSpam, "  link judi di deskripsi  ")
	require.NoError(t, err)
	assert.Equal(t, now.Truncate(time.Microsecond), at)
	assert.Equal(t, Report{VideoID: v.ID, ReporterID: 9, Reason: ReportSpam, Details: "link judi di deskripsi", ReportedAt: at},
		repo.reports[[2]int64{v.ID, 9}])

	// Melapor lagi mengganti laporan yang sama, bukan menambah.
	_, err = svc.ReportVideo(ctx, 9, v.ID, ReportCopyright, "")
	require.NoError(t, err)
	require.Len(t, repo.reports, 1)
	assert.Equal(t, ReportCopyright, repo.reports[[2]int64{v.ID, 9}].Reason)
}

func TestReportVideoRejects(t *testing.T) {
	ctx := context.Background()
	svc, repo, store := newTestService()
	v := readyVideo(repo, 7, time.Now())
	notReady := uploadedVideo(t, svc, store)

	var verr *ValidationError
	for _, tc := range []struct {
		name    string
		user    int64
		video   int64
		reason  ReportReason
		details string
		field   string
	}{
		{"alasan tidak dikenal", 9, v.ID, "bosan", "", "reason"},
		{"alasan kosong", 9, v.ID, "", "", "reason"},
		{"lainnya tanpa keterangan", 9, v.ID, ReportOther, "   ", "details"},
		{"keterangan terlalu panjang", 9, v.ID, ReportHate, strings.Repeat("é", 501), "details"},
		{"video sendiri", 7, v.ID, ReportSpam, "", "video_id"},
	} {
		_, err := svc.ReportVideo(ctx, tc.user, tc.video, tc.reason, tc.details)
		require.ErrorAs(t, err, &verr, tc.name)
		assert.Equal(t, tc.field, verr.Field, tc.name)
	}

	// 500 karakter (bukan byte) masih boleh.
	_, err := svc.ReportVideo(ctx, 9, v.ID, ReportOther, strings.Repeat("é", 500))
	require.NoError(t, err)

	for _, id := range []int64{0, 999, notReady.ID} {
		_, err := svc.ReportVideo(ctx, 9, id, ReportSpam, "")
		assert.ErrorIs(t, err, ErrNotFound, "video %d", id)
	}
	_, err = svc.ReportVideo(ctx, 0, v.ID, ReportSpam, "")
	assert.Error(t, err)
}
