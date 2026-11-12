package httpserver

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/time/rate"
)

// Langkah 28: pembatas laju request (rate limit) di gateway.
//
// Setiap kunci (IP untuk endpoint tanpa login, id user untuk endpoint yang
// butuh login) punya "ember token" sendiri: ember berisi `burst` token,
// setiap request memakai satu token, dan token terisi lagi sebanyak `per`.
// Jika ember kosong, gateway menjawab 429 dengan header Retry-After.
//
// Penyimpanannya di memori pod. Dengan 2 pod gateway, batas efektifnya dua
// kali lipat, dan batas mulai dari nol lagi setelah pod restart. Untuk
// aplikasi sebesar ini cukup; jika nanti butuh batas bersama antar-pod,
// pindahkan ke Redis atau ke Gateway (NGINX limit_req).
//
// Padanannya: express-rate-limit di Node.js, Bucket4j/Resilience4j
// RateLimiter di Java. Dokumentasi: https://pkg.go.dev/golang.org/x/time/rate

// rateRule adalah satu aturan: burst request sekaligus, lalu satu request
// lagi setiap `every`.
type rateRule struct {
	name  string
	every time.Duration
	burst int
}

// Aturan yang dipakai router. Angkanya untuk pemakaian manusia biasa:
// user normal tidak pernah menyentuhnya, skrip yang menebak password atau
// membanjiri komentar langsung tertahan.
var (
	// Login, daftar, lupa/reset password, tukar kode OAuth: per IP.
	// 10 percobaan sekaligus, lalu 1 per 6 detik (10 per menit).
	ruleAuth = rateRule{name: "auth", every: 6 * time.Second, burst: 10}
	// Refresh token dipanggil otomatis oleh frontend (setiap tab), jadi
	// lebih longgar: 30 sekaligus, lalu 1 per 2 detik. Per IP.
	ruleRefresh = rateRule{name: "refresh", every: 2 * time.Second, burst: 30}
	// Membuat video baru (minta URL upload): 10 sekaligus, lalu 1 per 3 menit. Per user.
	ruleUpload = rateRule{name: "upload", every: 3 * time.Minute, burst: 10}
	// Upload gambar (thumbnail, foto profil): 10 sekaligus, lalu 1 per 10 detik. Per user.
	ruleImage = rateRule{name: "image", every: 10 * time.Second, burst: 10}
	// Komentar: 10 sekaligus, lalu 1 per 3 detik. Per user.
	ruleComment = rateRule{name: "comment", every: 3 * time.Second, burst: 10}
	// Danmaku dan chat siaran: service sudah punya batas sendiri; ini hanya
	// penahan murah di depan. 10 sekaligus, lalu 1 per detik. Per user.
	ruleChat = rateRule{name: "chat", every: time.Second, burst: 10}
	// Laporan video: 5 sekaligus, lalu 1 per 30 detik. Per user.
	ruleReport = rateRule{name: "report", every: 30 * time.Second, burst: 5}
)

// limiterStore menyimpan satu rate.Limiter per kunci.
type limiterStore struct {
	rule rateRule
	now  func() time.Time

	mu      sync.Mutex
	entries map[string]*limiterEntry
	// lastSweep: kunci yang lama tidak dipakai dibuang paling sering sekali
	// per menit, supaya memori tidak terus bertambah.
	lastSweep time.Time
}

type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// maxLimiterKeys membatasi memori: kira-kira 200 byte per kunci, jadi
// 100.000 kunci sekitar 20 MB. Jika penuh (misalnya serangan dari banyak
// IP), semua ember dikosongkan dan mulai dari awal.
const maxLimiterKeys = 100_000

func newLimiterStore(rule rateRule, now func() time.Time) *limiterStore {
	return &limiterStore{rule: rule, now: now, entries: make(map[string]*limiterEntry)}
}

// allow mengambil satu token untuk key. Jika ember kosong, hasilnya false
// beserta lama menunggu sampai token berikutnya tersedia.
func (s *limiterStore) allow(key string) (bool, time.Duration) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sweep(now)
	e, ok := s.entries[key]
	if !ok {
		if len(s.entries) >= maxLimiterKeys {
			s.entries = make(map[string]*limiterEntry)
		}
		e = &limiterEntry{limiter: rate.NewLimiter(rate.Every(s.rule.every), s.rule.burst)}
		s.entries[key] = e
	}
	e.lastSeen = now

	r := e.limiter.ReserveN(now, 1)
	if delay := r.DelayFrom(now); delay > 0 {
		// Token tidak dipakai: request ini ditolak, bukan ditunda.
		r.CancelAt(now)
		return false, delay
	}
	return true, 0
}

// sweep membuang kunci yang embernya sudah penuh lagi (tidak dipakai
// selama waktu isi ulang penuh). Kunci seperti itu sama saja dengan kunci
// baru, jadi aman dibuang.
func (s *limiterStore) sweep(now time.Time) {
	if now.Sub(s.lastSweep) < time.Minute {
		return
	}
	s.lastSweep = now
	idle := s.rule.every * time.Duration(s.rule.burst)
	for k, e := range s.entries {
		if now.Sub(e.lastSeen) > idle {
			delete(s.entries, k)
		}
	}
}

// rateLimiter membuat middleware untuk setiap aturan. Jika dimatikan
// (RATE_LIMIT_ENABLED=false, atau di test), middleware-nya tidak berbuat
// apa-apa.
type rateLimiter struct {
	enabled bool
	now     func() time.Time
	limited *prometheus.CounterVec
}

func newRateLimiter(enabled bool, reg prometheus.Registerer) *rateLimiter {
	rl := &rateLimiter{enabled: enabled, now: time.Now}
	if reg != nil {
		rl.limited = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "icestream_http_rate_limited_total",
			Help: "Jumlah request yang ditolak rate limit (429) per aturan.",
		}, []string{"rule"})
		reg.MustRegister(rl.limited)
	}
	return rl
}

// perIP membatasi per alamat IP klien. c.ClientIP() hanya membaca
// X-Forwarded-For dari proxy di TRUSTED_PROXIES, jadi klien tidak bisa
// memalsukan IP-nya untuk lolos.
func (rl *rateLimiter) perIP(rule rateRule) gin.HandlerFunc {
	return rl.middleware(rule, func(c *gin.Context) string { return c.ClientIP() })
}

// perUser membatasi per user yang login. Dipasang SETELAH RequireAuth.
func (rl *rateLimiter) perUser(rule rateRule) gin.HandlerFunc {
	return rl.middleware(rule, func(c *gin.Context) string {
		if claims, ok := CurrentUser(c); ok {
			return "u:" + claims.Subject
		}
		return "ip:" + c.ClientIP()
	})
}

func (rl *rateLimiter) middleware(rule rateRule, key func(*gin.Context) string) gin.HandlerFunc {
	if !rl.enabled {
		return func(c *gin.Context) { c.Next() }
	}
	store := newLimiterStore(rule, func() time.Time { return rl.now() })
	return func(c *gin.Context) {
		ok, wait := store.allow(key(c))
		if ok {
			c.Next()
			return
		}
		if rl.limited != nil {
			rl.limited.WithLabelValues(rule.name).Inc()
		}
		seconds := int(math.Ceil(wait.Seconds()))
		c.Header("Retry-After", strconv.Itoa(seconds))
		abortError(c, http.StatusTooManyRequests, "RESOURCE_EXHAUSTED",
			"terlalu banyak permintaan, coba lagi dalam "+strconv.Itoa(seconds)+" detik")
	}
}
