package transcoder

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// jobMetrics menghitung hasil setiap tugas transcoding (langkah 28).
type jobMetrics struct {
	jobs     *prometheus.CounterVec
	duration prometheus.Histogram
}

// Hasil tugas untuk label result.
const (
	resultSucceeded   = "succeeded"   // HLS selesai dan event terkirim
	resultFailed      = "failed"      // video ditandai FAILED
	resultRetry       = "retry"       // gagal sementara, dicoba lagi
	resultInterrupted = "interrupted" // pod berhenti di tengah jalan
	resultInvalid     = "invalid"     // pesan NATS rusak, dibuang
)

// EnableMetrics mendaftarkan metrik transcoding ke reg. Panggil sekali
// sebelum Worker mulai menerima pesan. Tanpa ini Worker tetap jalan,
// hanya tidak mencatat metrik (dipakai di test).
func (w *Worker) EnableMetrics(reg prometheus.Registerer) {
	m := &jobMetrics{
		jobs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "icestream_transcode_jobs_total",
			Help: "Jumlah tugas transcoding per hasil.",
		}, []string{"result"}),
		duration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "icestream_transcode_duration_seconds",
			Help: "Lama transcoding yang berhasil, dari unduh sampai unggah HLS.",
			// 10 detik sampai 1 jam: video pendek sampai video panjang.
			Buckets: []float64{10, 30, 60, 120, 300, 600, 1200, 1800, 3600},
		}),
	}
	reg.MustRegister(m.jobs, m.duration)
	w.metrics = m
}

func (w *Worker) countJob(result string) {
	if w.metrics != nil {
		w.metrics.jobs.WithLabelValues(result).Inc()
	}
}

func (w *Worker) observeSuccess(d time.Duration) {
	if w.metrics != nil {
		w.metrics.duration.Observe(d.Seconds())
	}
}
