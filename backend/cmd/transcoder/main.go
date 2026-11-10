// Command transcoder menjalankan worker Ice-Stream yang mengubah video
// mentah menjadi HLS. Tidak punya API: ia hanya menunggu event
// videos.uploaded dari NATS JetStream, memprosesnya dengan ffmpeg, lalu
// melaporkan hasilnya lewat event videos.transcode.*.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/farhantaufikrahman/ice-stream/backend/internal/config"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/events"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/transcoder"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(logger); err != nil {
		logger.Error("transcoder berhenti karena error", slog.Any("error", err))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.LoadTranscoder()
	if err != nil {
		return err
	}
	// Gagal sejak awal jika ffmpeg tidak ada, daripada gagal saat video
	// pertama datang.
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			return errors.New(bin + " tidak ditemukan di PATH")
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	storage := transcoder.NewS3Storage(cfg.Storage)

	nc, err := events.Connect(cfg.NATSURL, "transcoder")
	if err != nil {
		return err
	}
	defer nc.Drain()
	js, err := jetstream.New(nc)
	if err != nil {
		return err
	}

	setupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := events.EnsureStream(setupCtx, js); err != nil {
		return err
	}
	cons, err := js.CreateOrUpdateConsumer(setupCtx, events.StreamName, jetstream.ConsumerConfig{
		Durable:       events.ConsumerTranscoder,
		FilterSubject: events.SubjectUploaded,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       transcoder.AckWait,
		MaxDeliver:    transcoder.MaxDeliver,
		// Hanya 1 video yang dikerjakan sekaligus, walau ada banyak antrean.
		// Kalau nanti ada 2 pod transcoder, masing-masing tetap 1 video.
		MaxAckPending: 1,
	})
	if err != nil {
		return err
	}

	worker := transcoder.NewWorker(
		transcoder.FFmpeg{Preset: cfg.FFmpegPreset, Threads: cfg.FFmpegThreads},
		storage,
		transcoder.JetStreamPublisher{JS: js},
		cfg.WorkDir, cfg.JobTimeout, logger,
	)
	cc, err := cons.Consume(func(msg jetstream.Msg) {
		worker.Handle(ctx, msg)
	}, jetstream.PullMaxMessages(1))
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           healthHandler(nc, storage),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server health berhenti", slog.Any("error", err))
			stop()
		}
	}()

	logger.Info("transcoder siap",
		slog.String("preset", cfg.FFmpegPreset),
		slog.Int("threads", cfg.FFmpegThreads),
		slog.String("health", cfg.HTTPAddr),
	)
	<-ctx.Done()
	logger.Info("sinyal berhenti diterima")

	// ctx sudah selesai, jadi ffmpeg yang sedang berjalan ikut dihentikan
	// dan videonya dikembalikan ke antrean (Nak). Tunggu sampai handler
	// benar-benar selesai.
	cc.Stop()
	select {
	case <-cc.Closed():
	case <-time.After(cfg.ShutdownTimeout):
		logger.Warn("batas waktu shutdown habis")
	}
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	_ = srv.Shutdown(shutdownCtx)
	logger.Info("transcoder berhenti dengan rapi")
	return nil
}

// healthHandler melayani dua endpoint untuk Kubernetes:
//   - GET /livez:   proses masih hidup (livenessProbe). Sengaja tidak
//     memeriksa NATS/storage, karena restart pod tidak membuat keduanya sembuh.
//   - GET /healthz: NATS dan storage bisa dihubungi (readinessProbe). Kalau
//     gagal, pod tampil 0/1 di `kubectl get pods`, jadi masalahnya kelihatan.
func healthHandler(nc *nats.Conn, storage *transcoder.S3Storage) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /livez", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if nc.Status() != nats.CONNECTED {
			http.Error(w, "nats: "+nc.Status().String(), http.StatusServiceUnavailable)
			return
		}
		if err := storage.Ping(ctx); err != nil {
			http.Error(w, "storage: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}