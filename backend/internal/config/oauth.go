package config

import (
	"fmt"
	"net/url"
	"strings"
)

// OAuthConfig mengatur "Masuk dengan Google / GitHub" di gateway
// (langkah 26). Penyedia yang client id-nya kosong dianggap mati, dan
// tombolnya tidak ditampilkan frontend.
type OAuthConfig struct {
	// RedirectBaseURL adalah alamat gateway yang didaftarkan sebagai
	// callback di Google/GitHub, misalnya "http://localhost:4000".
	// Callback = RedirectBaseURL + "/api/v1/auth/oauth/<provider>/callback".
	// Google hanya menerima http untuk host "localhost", jadi alamat ini
	// bisa berbeda dengan alamat aplikasi.
	RedirectBaseURL string
	// AppBaseURL adalah alamat frontend tujuan setelah login, misalnya
	// "http://ice-stream.localhost:4000" (variabel yang sama dengan
	// tautan email di AuthService).
	AppBaseURL string
	Google     OAuthProvider
	GitHub     OAuthProvider
}

// OAuthProvider berisi kredensial satu aplikasi OAuth.
type OAuthProvider struct {
	ClientID     string
	ClientSecret string
}

// Enabled: client id dan secret sama-sama terisi.
func (p OAuthProvider) Enabled() bool {
	return p.ClientID != "" && p.ClientSecret != ""
}

func loadOAuth() OAuthConfig {
	return OAuthConfig{
		RedirectBaseURL: strings.TrimRight(getString("OAUTH_REDIRECT_BASE_URL", "http://localhost:8080"), "/"),
		AppBaseURL:      strings.TrimRight(getString("APP_BASE_URL", "http://localhost:5173"), "/"),
		Google: OAuthProvider{
			ClientID:     getString("OAUTH_GOOGLE_CLIENT_ID", ""),
			ClientSecret: getString("OAUTH_GOOGLE_CLIENT_SECRET", ""),
		},
		GitHub: OAuthProvider{
			ClientID:     getString("OAUTH_GITHUB_CLIENT_ID", ""),
			ClientSecret: getString("OAUTH_GITHUB_CLIENT_SECRET", ""),
		},
	}
}

func (o OAuthConfig) validate() error {
	for key, raw := range map[string]string{"OAUTH_REDIRECT_BASE_URL": o.RedirectBaseURL, "APP_BASE_URL": o.AppBaseURL} {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" || u.RawQuery != "" {
			return fmt.Errorf("%s harus berupa alamat http(s) tanpa path, dapat %q", key, raw)
		}
	}
	// Hanya salah satu yang terisi hampir pasti salah ketik di Secret.
	for name, p := range map[string]OAuthProvider{"GOOGLE": o.Google, "GITHUB": o.GitHub} {
		if (p.ClientID == "") != (p.ClientSecret == "") {
			return fmt.Errorf("OAUTH_%s_CLIENT_ID dan OAUTH_%s_CLIENT_SECRET harus diisi berdua atau dikosongkan berdua", name, name)
		}
	}
	return nil
}
