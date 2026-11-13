// Package mail mengirim email lewat SMTP (langkah 24c).
//
// Di cluster lokal, SMTP-nya adalah Mailpit: semua email ditangkap dan
// bisa dibaca di http://mail.ice-stream.localhost:4000, tidak ada yang
// benar-benar terkirim ke internet. Versi online (langkah 29) memakai
// Resend (smtp.resend.com:465, TLS + login).
//
// Dibandingkan Node (nodemailer) atau Java (Spring JavaMailSender), Go
// sudah punya net/smtp di library standar:
// https://pkg.go.dev/net/smtp
package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

// Message adalah satu email teks biasa.
type Message struct {
	To      string
	Subject string
	// Text adalah isi email (UTF-8, baris dipisah "\n").
	Text string
}

// Cara mengamankan koneksi SMTP (SMTP_TLS).
const (
	// TLSNone: tanpa enkripsi, hanya untuk Mailpit di dalam cluster.
	TLSNone = "none"
	// TLSStartTLS: sambung biasa lalu "naik" ke TLS (port 587).
	TLSStartTLS = "starttls"
	// TLSImplicit: TLS sejak awal koneksi (port 465).
	TLSImplicit = "tls"
)

// SMTPSender mengirim email ke server SMTP. Tanpa Username dan dengan
// TLS = TLSNone cocok untuk Mailpit; layanan sungguhan (Resend) butuh
// TLS dan login.
type SMTPSender struct {
	// Addr adalah host:port server SMTP, misalnya "mailpit:1025".
	Addr string
	// From adalah pengirim, misalnya "Ice-Stream <no-reply@ice-stream.localhost>".
	From string
	// Timeout membatasi seluruh pengiriman (sambung + kirim).
	Timeout time.Duration
	// TLS adalah TLSNone (default jika kosong), TLSStartTLS, atau TLSImplicit.
	TLS string
	// Username dan Password untuk login SMTP (AUTH PLAIN). Kosong = tanpa login.
	Username string
	Password string
	// tlsConfig hanya diisi tes (sertifikat buatan sendiri).
	tlsConfig *tls.Config
}

func (s SMTPSender) clientTLS(host string) *tls.Config {
	if s.tlsConfig != nil {
		return s.tlsConfig
	}
	return &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
}

// Send mengirim satu email.
func (s SMTPSender) Send(ctx context.Context, m Message) error {
	from, err := mail.ParseAddress(s.From)
	if err != nil {
		return fmt.Errorf("MAIL_FROM tidak valid: %w", err)
	}
	to, err := mail.ParseAddress(m.To)
	if err != nil {
		return fmt.Errorf("alamat tujuan tidak valid: %w", err)
	}
	body, err := buildMessage(from, to, m, time.Now())
	if err != nil {
		return err
	}

	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// smtp.SendMail tidak menerima context, jadi koneksinya dibuat sendiri
	// supaya bisa diberi batas waktu.
	host, _, _ := net.SplitHostPort(s.Addr)
	var conn net.Conn
	if s.TLS == TLSImplicit {
		d := tls.Dialer{Config: s.clientTLS(host)}
		conn, err = d.DialContext(ctx, "tcp", s.Addr)
	} else {
		var d net.Dialer
		conn, err = d.DialContext(ctx, "tcp", s.Addr)
	}
	if err != nil {
		return fmt.Errorf("sambung ke SMTP %s: %w", s.Addr, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("SMTP hello: %w", err)
	}
	defer c.Close()

	if s.TLS == TLSStartTLS {
		// Server yang tidak menawarkan STARTTLS ditolak, supaya password
		// tidak pernah terkirim tanpa enkripsi.
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("server SMTP tidak mendukung STARTTLS")
		}
		if err := c.StartTLS(s.clientTLS(host)); err != nil {
			return fmt.Errorf("SMTP STARTTLS: %w", err)
		}
	}
	if s.Username != "" {
		// smtp.PlainAuth sendiri menolak mengirim password lewat koneksi
		// tanpa TLS (kecuali ke localhost).
		if err := c.Auth(smtp.PlainAuth("", s.Username, s.Password, host)); err != nil {
			return fmt.Errorf("SMTP login: %w", err)
		}
	}

	if err := c.Mail(from.Address); err != nil {
		return fmt.Errorf("SMTP MAIL FROM: %w", err)
	}
	if err := c.Rcpt(to.Address); err != nil {
		return fmt.Errorf("SMTP RCPT TO: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("tulis isi email: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("SMTP kirim: %w", err)
	}
	return c.Quit()
}

// buildMessage menyusun email lengkap (header + isi) sesuai RFC 5322.
// Judul berhuruf non-ASCII dikodekan dengan mime.QEncoding, dan isinya
// memakai quoted-printable supaya aman dikirim lewat server SMTP mana pun.
func buildMessage(from, to *mail.Address, m Message, now time.Time) ([]byte, error) {
	if strings.ContainsAny(m.Subject, "\r\n") {
		// Mencegah header injection lewat judul.
		return nil, errors.New("judul email tidak boleh berisi baris baru")
	}
	id := make([]byte, 12)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	domain := "localhost"
	if at := strings.LastIndex(from.Address, "@"); at >= 0 {
		domain = from.Address[at+1:]
	}

	var b bytes.Buffer
	header := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	header("From", from.String())
	header("To", to.String())
	header("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	header("Date", now.Format(time.RFC1123Z))
	header("Message-ID", "<"+hex.EncodeToString(id)+"@"+domain+">")
	header("MIME-Version", "1.0")
	header("Content-Type", "text/plain; charset=UTF-8")
	header("Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")

	qp := quotedprintable.NewWriter(&b)
	if _, err := qp.Write([]byte(strings.ReplaceAll(m.Text, "\n", "\r\n"))); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
