package transcoder

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestWorkerMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	w, _ := newTestWorker(t, &fakeMedia{}, &fakeStorage{})
	w.EnableMetrics(reg)
	w.Handle(context.Background(), newMsg(t, uploaded, 1))

	bad, _ := newTestWorker(t, &fakeMedia{transcodeErr: errors.New("ffmpeg kehabisan memori")}, &fakeStorage{})
	bad.metrics = w.metrics // registry yang sama
	bad.Handle(context.Background(), newMsg(t, uploaded, 1))
	bad.Handle(context.Background(), newMsg(t, uploaded, MaxAttempts))

	assert.Equal(t, 1.0, testutil.ToFloat64(w.metrics.jobs.WithLabelValues(resultSucceeded)))
	assert.Equal(t, 1.0, testutil.ToFloat64(w.metrics.jobs.WithLabelValues(resultRetry)))
	assert.Equal(t, 1.0, testutil.ToFloat64(w.metrics.jobs.WithLabelValues(resultFailed)))
	n, err := testutil.GatherAndCount(reg, "icestream_transcode_duration_seconds")
	assert.NoError(t, err)
	assert.Equal(t, 1, n)
}
