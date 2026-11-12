package livestream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Bagian ini berbicara dengan MediaMTX (https://mediamtx.org) lewat HTTP:
//
//  1. MediaMTXClient membaca daftar stream dari Control API MediaMTX
//     (GET /v3/paths/list). Dokumentasi:
//     https://mediamtx.org/docs/features/control-api
//  2. HookHandler menjawab pertanyaan MediaMTX "boleh siaran?" setiap kali
//     OBS mulai mengirim video (authMethod: http). Dokumentasi:
//     https://mediamtx.org/docs/features/authentication
//  3. Syncer menjalankan (1) setiap beberapa detik lalu memanggil
//     Service.Sync.
//
// Padanannya di Node: memanggil API dengan fetch/axios dalam setInterval,
// dan route Express untuk hook-nya. Di Java: RestClient + @Scheduled.

// MediaMTXClient memanggil Control API MediaMTX.
type MediaMTXClient struct {
	// BaseURL misalnya "http://mediamtx:9997".
	BaseURL string
	HTTP    *http.Client
}

// pathsPage adalah sebagian jawaban GET /v3/paths/list yang kita pakai.
// Bentuk lengkapnya ada di api/openapi.yaml repo MediaMTX.
type pathsPage struct {
	PageCount int `json:"pageCount"`
	Items     []struct {
		Name      string    `json:"name"`
		Ready     bool      `json:"ready"`
		ReadyTime time.Time `json:"readyTime"`
		// Readers berisi satu entri per penonton (tipe "hlsSession" untuk
		// HLS).
		Readers []struct {
			Type string `json:"type"`
		} `json:"readers"`
	} `json:"items"`
}

// maxPathPages membatasi berapa halaman yang dibaca dalam satu putaran
// (100 stream per halaman), supaya bug di MediaMTX tidak membuat loop tanpa
// akhir.
const maxPathPages = 50

// Online membaca semua stream "live/<id>" yang sedang menerima video.
func (c MediaMTXClient) Online(ctx context.Context) ([]Online, error) {
	var out []Online
	for page := 0; page < maxPathPages; page++ {
		var p pathsPage
		url := c.BaseURL + "/v3/paths/list?itemsPerPage=100&page=" + strconv.Itoa(page)
		if err := c.getJSON(ctx, url, &p); err != nil {
			return nil, err
		}
		for _, it := range p.Items {
			ownerID, ok := OwnerFromPath(it.Name)
			if !ok || !it.Ready {
				continue
			}
			out = append(out, Online{OwnerID: ownerID, ReadySince: it.ReadyTime.UTC(), Viewers: int64(len(it.Readers))})
		}
		// Halaman dimulai dari 0.
		if page+1 >= p.PageCount {
			return out, nil
		}
	}
	return out, nil
}

func (c MediaMTXClient) getJSON(ctx context.Context, url string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("panggil MediaMTX: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("MediaMTX menjawab HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(v); err != nil {
		return fmt.Errorf("baca jawaban MediaMTX: %w", err)
	}
	return nil
}

// hookRequest adalah isi POST dari MediaMTX ke authHTTPAddress.
type hookRequest struct {
	Action   string `json:"action"`
	Path     string `json:"path"`
	Protocol string `json:"protocol"`
	Query    string `json:"query"`
	IP       string `json:"ip"`
	ID       string `json:"id"`
}

// Authorizer adalah bagian Service yang dipakai HookHandler.
type Authorizer interface {
	AuthorizePublish(ctx context.Context, path, query string) (int64, error)
}

// HookHandler membuat handler HTTP untuk POST /mediamtx/auth.
//
// MediaMTX menganggap jawaban 2xx = boleh, selain itu = ditolak. Hanya
// action "publish" yang dikirim ke sini: "read" dan "playback" dikecualikan
// di konfigurasi MediaMTX (authHTTPExclude), jadi penonton tidak membebani
// Oracle. Jika action lain tetap datang, jawabannya ditolak.
func HookHandler(auth Authorizer, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /mediamtx/auth", func(w http.ResponseWriter, r *http.Request) {
		var req hookRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
			http.Error(w, "body tidak valid", http.StatusBadRequest)
			return
		}
		if req.Action != "publish" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		ownerID, err := auth.AuthorizePublish(r.Context(), req.Path, req.Query)
		switch {
		case err == nil:
			logger.InfoContext(r.Context(), "siaran diizinkan", slog.Int64("owner_id", ownerID), slog.String("protocol", req.Protocol))
			w.WriteHeader(http.StatusOK)
		case errors.Is(err, ErrForbidden):
			// Query (berisi key) sengaja tidak dicatat.
			logger.WarnContext(r.Context(), "siaran ditolak", slog.String("path", req.Path), slog.String("ip", req.IP))
			w.WriteHeader(http.StatusUnauthorized)
		default:
			logger.ErrorContext(r.Context(), "hook auth MediaMTX gagal", slog.Any("error", err))
			w.WriteHeader(http.StatusInternalServerError)
		}
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

// Syncer menyamakan status siaran dengan MediaMTX secara berkala.
type Syncer struct {
	Source interface {
		Online(ctx context.Context) ([]Online, error)
	}
	Service interface {
		Sync(ctx context.Context, online []Online) (started, ended int, err error)
	}
	Interval time.Duration
	Logger   *slog.Logger
}

// Run berjalan sampai ctx dibatalkan.
func (s Syncer) Run(ctx context.Context) {
	t := time.NewTicker(s.Interval)
	defer t.Stop()
	var failing bool
	for {
		err := s.tick(ctx)
		// Error yang sama tidak dicatat berulang-ulang setiap 5 detik:
		// hanya saat mulai gagal dan saat pulih.
		switch {
		case err != nil && !failing:
			s.Logger.Warn("gagal membaca status siaran dari MediaMTX", slog.Any("error", err))
		case err == nil && failing:
			s.Logger.Info("status siaran dari MediaMTX terbaca lagi")
		}
		failing = err != nil
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s Syncer) tick(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, s.Interval)
	defer cancel()
	online, err := s.Source.Online(ctx)
	if err != nil {
		// MediaMTX tidak bisa dihubungi: jangan menutup siaran apa pun,
		// karena kita tidak tahu apakah kreatornya masih live.
		return err
	}
	started, ended, err := s.Service.Sync(ctx, online)
	if err != nil {
		return err
	}
	if started > 0 || ended > 0 {
		owners := make([]string, len(online))
		for i, o := range online {
			owners[i] = strconv.FormatInt(o.OwnerID, 10)
		}
		s.Logger.Info("status siaran berubah", slog.Int("mulai", started), slog.Int("selesai", ended),
			slog.String("sedang_live", strings.Join(owners, ",")))
	}
	return nil
}
