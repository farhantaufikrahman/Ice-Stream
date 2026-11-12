package httpserver

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/auth/v1"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/autherr"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/config"
)

// "Masuk dengan Google / GitHub" (langkah 26), alur OAuth 2.0
// Authorization Code dengan PKCE:
//
//  1. Frontend membuka start_url dari GET /auth/oauth/providers (pindah
//     halaman biasa, bukan fetch).
//  2. start: gateway menyimpan state + code_verifier di cookie sementara,
//     lalu mengarahkan browser ke halaman izin Google/GitHub.
//  3. callback: penyedia mengembalikan browser dengan ?code&state. Gateway
//     mencocokkan state dengan cookie (mencegah CSRF), menukar code dengan
//     access token penyedia, membaca profil user, lalu meminta AuthService
//     membuat kode login sekali pakai.
//  4. Browser diarahkan ke APP_BASE_URL/oauth/selesai#code=..., dan
//     frontend menukar kode itu lewat POST /auth/oauth/exchange (cookie
//     refresh token dipasang di alamat aplikasi).
//
// Langkah 4 diperlukan karena alamat callback bisa berbeda dengan alamat
// aplikasi: Google hanya menerima http untuk host "localhost". Kode
// ditaruh di fragment (#), jadi tidak ikut terkirim ke server mana pun
// dan tidak masuk log akses.
//
// Padanan di ekosistem lain: Passport.js (Express), Spring Security
// OAuth2 Client (Java), atau golang.org/x/oauth2 yang dipakai di sini.

const (
	oauthCookieName = "ice_oauth"
	oauthCookiePath = "/api/v1/auth/oauth"
	// oauthCookieTTL: waktu yang diberikan untuk menyelesaikan halaman izin.
	oauthCookieTTL = 10 * time.Minute
	// oauthCallTimeout membatasi penukaran code dan pembacaan profil.
	oauthCallTimeout = 10 * time.Second
	// maxProviderBody membatasi jawaban API profil penyedia.
	maxProviderBody = 1 << 20
)

// oauthProvider berisi alamat satu penyedia. Field URL bisa diganti di
// test dengan server palsu (httptest).
type oauthProvider struct {
	ID     string
	config oauth2.Config
	// fetchIdentity membaca profil user memakai access token penyedia.
	fetchIdentity func(ctx context.Context, client *http.Client) (*authv1.LoginWithIdentityRequest, error)
}

// oauthHandler menjalankan alur OAuth. providers hanya berisi penyedia
// yang dinyalakan (client id dan secret terisi).
type oauthHandler struct {
	auth            authv1.AuthServiceClient
	providers       map[string]*oauthProvider
	order           []string
	redirectBaseURL string
	appBaseURL      string
	secureCookie    bool
	refresh         *authHandler
	httpClient      *http.Client
	logger          *slog.Logger
}

// API profil penyedia. Diambil dari dokumentasi resmi:
//   - Google: userinfo_endpoint di
//     https://accounts.google.com/.well-known/openid-configuration
//   - GitHub: https://docs.github.com/en/rest/users/users (GET /user) dan
//     https://docs.github.com/en/rest/users/emails (GET /user/emails,
//     butuh scope user:email).
//
// Alamat halaman izin dan token sama dengan endpoints.Google dan
// endpoints.GitHub di golang.org/x/oauth2/endpoints. Ditulis sebagai
// string biasa supaya bisa diganti saat build untuk uji lokal dengan
// penyedia palsu: go build -ldflags "-X <package>.githubAuthURL=...".
var (
	googleAuthURL     = "https://accounts.google.com/o/oauth2/auth"
	googleTokenURL    = "https://oauth2.googleapis.com/token"
	googleUserInfoURL = "https://openidconnect.googleapis.com/v1/userinfo"
	githubAuthURL     = "https://github.com/login/oauth/authorize"
	githubTokenURL    = "https://github.com/login/oauth/access_token"
	githubAPIURL      = "https://api.github.com"
)

func newOAuthHandler(cfg config.OAuthConfig, auth authv1.AuthServiceClient, refresh *authHandler, logger *slog.Logger) *oauthHandler {
	h := &oauthHandler{
		auth:            auth,
		providers:       map[string]*oauthProvider{},
		redirectBaseURL: cfg.RedirectBaseURL,
		appBaseURL:      cfg.AppBaseURL,
		secureCookie:    strings.HasPrefix(cfg.RedirectBaseURL, "https://"),
		refresh:         refresh,
		httpClient:      &http.Client{Timeout: oauthCallTimeout},
		logger:          logger,
	}
	callback := func(id string) string {
		return cfg.RedirectBaseURL + "/api/v1/auth/oauth/" + id + "/callback"
	}
	if cfg.Google.Enabled() {
		h.add(&oauthProvider{
			ID: "google",
			config: oauth2.Config{
				ClientID:     cfg.Google.ClientID,
				ClientSecret: cfg.Google.ClientSecret,
				Endpoint:     oauth2.Endpoint{AuthURL: googleAuthURL, TokenURL: googleTokenURL},
				RedirectURL:  callback("google"),
				Scopes:       []string{"openid", "email", "profile"},
			},
			fetchIdentity: fetchGoogleIdentity,
		})
	}
	if cfg.GitHub.Enabled() {
		h.add(&oauthProvider{
			ID: "github",
			config: oauth2.Config{
				ClientID:     cfg.GitHub.ClientID,
				ClientSecret: cfg.GitHub.ClientSecret,
				Endpoint:     oauth2.Endpoint{AuthURL: githubAuthURL, TokenURL: githubTokenURL},
				RedirectURL:  callback("github"),
				Scopes:       []string{"read:user", "user:email"},
			},
			fetchIdentity: fetchGitHubIdentity,
		})
	}
	return h
}

func (h *oauthHandler) add(p *oauthProvider) {
	h.providers[p.ID] = p
	h.order = append(h.order, p.ID)
}

type oauthProviderResponse struct {
	ID string `json:"id"`
	// StartURL dibuka dengan pindah halaman (window.location.href).
	StartURL string `json:"start_url"`
}

// listProviders: GET /api/v1/auth/oauth/providers. Hanya penyedia yang
// dinyalakan, jadi frontend tidak menampilkan tombol yang tidak berfungsi.
func (h *oauthHandler) listProviders(c *gin.Context) {
	out := make([]oauthProviderResponse, 0, len(h.order))
	for _, id := range h.order {
		out = append(out, oauthProviderResponse{
			ID:       id,
			StartURL: h.redirectBaseURL + "/api/v1/auth/oauth/" + id + "/start",
		})
	}
	c.JSON(http.StatusOK, gin.H{"providers": out})
}

// oauthState disimpan di cookie selama halaman izin terbuka.
type oauthState struct {
	State    string `json:"s"`
	Verifier string `json:"v"`
	Provider string `json:"p"`
	// Next adalah halaman aplikasi tujuan setelah login, misalnya "/studio".
	Next string `json:"n"`
}

// start: GET /api/v1/auth/oauth/:provider/start?next=/studio
func (h *oauthHandler) start(c *gin.Context) {
	p, ok := h.providers[c.Param("provider")]
	if !ok {
		abortError(c, http.StatusNotFound, "NOT_FOUND", "penyedia login tidak dikenal atau belum dinyalakan")
		return
	}
	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		abortError(c, http.StatusInternalServerError, "INTERNAL", "terjadi kesalahan internal")
		return
	}
	st := oauthState{
		State:    base64.RawURLEncoding.EncodeToString(stateBytes),
		Verifier: oauth2.GenerateVerifier(),
		Provider: p.ID,
		Next:     safeNext(c.Query("next")),
	}
	raw, _ := json.Marshal(st)
	http.SetCookie(c.Writer, &http.Cookie{
		Name:  oauthCookieName,
		Value: base64.RawURLEncoding.EncodeToString(raw),
		Path:  oauthCookiePath,
		// Lax: cookie tetap terkirim saat penyedia mengarahkan browser
		// kembali (navigasi GET tingkat atas dari situs lain).
		MaxAge:   int(oauthCookieTTL.Seconds()),
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteLaxMode,
	})
	c.Redirect(http.StatusFound, p.config.AuthCodeURL(st.State, oauth2.S256ChallengeOption(st.Verifier)))
}

// callback: GET /api/v1/auth/oauth/:provider/callback?code=...&state=...
// Semua hasil (berhasil atau gagal) berupa redirect ke aplikasi, karena
// yang membuka alamat ini adalah browser, bukan JavaScript.
func (h *oauthHandler) callback(c *gin.Context) {
	p, ok := h.providers[c.Param("provider")]
	if !ok {
		abortError(c, http.StatusNotFound, "NOT_FOUND", "penyedia login tidak dikenal atau belum dinyalakan")
		return
	}
	st, ok := h.readState(c)
	// Cookie dihapus apa pun hasilnya: state hanya boleh dipakai sekali.
	h.clearStateCookie(c)
	if !ok || st.Provider != p.ID ||
		subtle.ConstantTimeCompare([]byte(st.State), []byte(c.Query("state"))) != 1 {
		h.finish(c, "", "", "state")
		return
	}
	if errCode := c.Query("error"); errCode != "" {
		// Misalnya access_denied: user menekan "Batal" di halaman izin.
		h.finish(c, "", st.Next, "denied")
		return
	}
	code := c.Query("code")
	if code == "" {
		h.finish(c, "", st.Next, "failed")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), oauthCallTimeout)
	defer cancel()
	// Klien HTTP dengan batas waktu dipakai untuk semua panggilan ke
	// penyedia (lihat dokumentasi oauth2.HTTPClient).
	ctx = context.WithValue(ctx, oauth2.HTTPClient, h.httpClient)
	token, err := p.config.Exchange(ctx, code, oauth2.VerifierOption(st.Verifier))
	if err != nil {
		h.logger.WarnContext(ctx, "gagal menukar code OAuth", slog.String("provider", p.ID), slog.Any("error", err))
		h.finish(c, "", st.Next, "failed")
		return
	}
	ident, err := p.fetchIdentity(ctx, p.config.Client(ctx, token))
	if err != nil {
		h.logger.WarnContext(ctx, "gagal membaca profil OAuth", slog.String("provider", p.ID), slog.Any("error", err))
		h.finish(c, "", st.Next, "failed")
		return
	}
	ident.Provider = p.ID
	resp, err := h.auth.LoginWithIdentity(ctx, ident)
	if err != nil {
		h.finish(c, "", st.Next, h.identityErrorCode(ctx, err))
		return
	}
	h.finish(c, resp.GetLoginCode(), st.Next, "")
}

// identityErrorCode memetakan error AuthService ke kode yang dipahami
// halaman /oauth/selesai.
func (h *oauthHandler) identityErrorCode(ctx context.Context, err error) string {
	st := status.Convert(err)
	switch st.Code() {
	case codes.PermissionDenied:
		return "banned"
	case codes.FailedPrecondition:
		for _, d := range st.Details() {
			info, ok := d.(*errdetails.ErrorInfo)
			if !ok || info.GetDomain() != autherr.Domain {
				continue
			}
			switch info.GetReason() {
			case autherr.ReasonEmailNotVerified:
				return "email_unverified"
			case autherr.ReasonEmailNeedsPassword:
				return "email_needs_password"
			case autherr.ReasonProviderAlreadyLinked:
				return "already_linked"
			}
		}
	}
	h.logger.ErrorContext(ctx, "LoginWithIdentity gagal", slog.Any("error", err))
	return "failed"
}

// finish mengarahkan browser ke halaman /oauth/selesai di aplikasi.
// Semua nilai ada di fragment (#), bukan query.
func (h *oauthHandler) finish(c *gin.Context, code, next, errCode string) {
	frag := url.Values{}
	if code != "" {
		frag.Set("code", code)
	}
	if next != "" {
		frag.Set("next", next)
	}
	if errCode != "" {
		frag.Set("error", errCode)
	}
	c.Header("Cache-Control", "no-store")
	c.Redirect(http.StatusFound, h.appBaseURL+"/oauth/selesai#"+frag.Encode())
}

func (h *oauthHandler) readState(c *gin.Context) (oauthState, bool) {
	raw, err := c.Cookie(oauthCookieName)
	if err != nil {
		return oauthState{}, false
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return oauthState{}, false
	}
	var st oauthState
	if json.Unmarshal(data, &st) != nil || st.State == "" || st.Verifier == "" {
		return oauthState{}, false
	}
	st.Next = safeNext(st.Next)
	return st, true
}

func (h *oauthHandler) clearStateCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     oauthCookieName,
		Value:    "",
		Path:     oauthCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteLaxMode,
	})
}

// safeNext hanya menerima path di aplikasi sendiri ("/studio"), bukan
// alamat situs lain ("//jahat.com", "https://..."), supaya tidak menjadi
// "open redirect".
func safeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") || len(next) > 500 {
		return ""
	}
	return next
}

type exchangeRequest struct {
	Code string `json:"code"`
}

// exchange: POST /api/v1/auth/oauth/exchange {"code": "..."}. Jawabannya
// sama dengan POST /auth/login, termasuk cookie refresh token.
func (h *oauthHandler) exchange(c *gin.Context) {
	var req exchangeRequest
	if !bindJSON(c, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), authCallTimeout)
	defer cancel()
	resp, err := h.auth.ExchangeLoginCode(ctx, &authv1.ExchangeLoginCodeRequest{LoginCode: req.Code, Client: clientInfo(c)})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	tokens := resp.GetTokens()
	h.refresh.setRefreshCookie(c, tokens)
	c.JSON(http.StatusOK, loginResponse{
		User:                toUserResponse(resp.GetUser()),
		accessTokenResponse: toAccessTokenResponse(tokens),
	})
}

// getJSON memanggil API penyedia dan membaca jawabannya ke out.
func getJSON(ctx context.Context, client *http.Client, rawURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxProviderBody))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", rawURL, resp.StatusCode)
	}
	return json.Unmarshal(body, out)
}

// fetchGoogleIdentity membaca klaim OpenID Connect standar dari userinfo.
func fetchGoogleIdentity(ctx context.Context, client *http.Client) (*authv1.LoginWithIdentityRequest, error) {
	var info struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := getJSON(ctx, client, googleUserInfoURL, &info); err != nil {
		return nil, err
	}
	if info.Sub == "" {
		return nil, errors.New("userinfo Google tanpa sub")
	}
	return &authv1.LoginWithIdentityRequest{
		Subject:       info.Sub,
		Email:         info.Email,
		EmailVerified: info.EmailVerified,
		Name:          info.Name,
	}, nil
}

// fetchGitHubIdentity: id dan login dari GET /user, email dari
// GET /user/emails (email utama yang sudah diverifikasi). Email di
// GET /user bisa kosong jika user menyembunyikannya, jadi tidak dipakai.
func fetchGitHubIdentity(ctx context.Context, client *http.Client) (*authv1.LoginWithIdentityRequest, error) {
	var u struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	if err := getJSON(ctx, client, githubAPIURL+"/user", &u); err != nil {
		return nil, err
	}
	if u.ID == 0 {
		return nil, errors.New("profil GitHub tanpa id")
	}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := getJSON(ctx, client, githubAPIURL+"/user/emails", &emails); err != nil {
		return nil, err
	}
	out := &authv1.LoginWithIdentityRequest{
		Subject:      strconv.FormatInt(u.ID, 10),
		Name:         u.Name,
		UsernameHint: u.Login,
	}
	for _, e := range emails {
		if e.Primary {
			out.Email, out.EmailVerified = e.Email, e.Verified
		}
	}
	return out, nil
}
