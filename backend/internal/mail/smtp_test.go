package mail

import (
	"bufio"
	"context"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMessage(t *testing.T) {
	from := &mail.Address{Name: "Ice-Stream", Address: "no-reply@ice-stream.localhost"}
	to := &mail.Address{Address: "budi@example.com"}
	raw, err := buildMessage(from, to, Message{To: to.Address, Subject: "Atur ulang kata sandi – Ice-Stream", Text: "Halo Budi,\nklik tautan ini."},
		time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	// Hasilnya bisa dibaca kembali oleh parser email standar Go.
	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	require.NoError(t, err)
	assert.Equal(t, `"Ice-Stream" <no-reply@ice-stream.localhost>`, msg.Header.Get("From"))
	subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	require.NoError(t, err)
	assert.Equal(t, "Atur ulang kata sandi – Ice-Stream", subject)
	assert.True(t, strings.HasSuffix(msg.Header.Get("Message-Id"), "@ice-stream.localhost>"))
	body, err := io.ReadAll(quotedprintable.NewReader(msg.Body))
	require.NoError(t, err)
	assert.Equal(t, "Halo Budi,\r\nklik tautan ini.", string(body))

	_, err = buildMessage(from, to, Message{Subject: "a\r\nBcc: x@y.z"}, time.Now())
	assert.Error(t, err, "baris baru di judul ditolak")
}

// TestSMTPSenderSend memakai server SMTP palsu kecil di localhost.
func TestSMTPSenderSend(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	got := make(chan string, 1)
	go fakeSMTP(t, ln, got)

	s := SMTPSender{Addr: ln.Addr().String(), From: "Ice-Stream <no-reply@ice-stream.localhost>", Timeout: 5 * time.Second}
	require.NoError(t, s.Send(context.Background(), Message{To: "budi@example.com", Subject: "Tes", Text: "isi"}))
	data := <-got
	assert.Contains(t, data, "MAIL FROM:<no-reply@ice-stream.localhost>")
	assert.Contains(t, data, "RCPT TO:<budi@example.com>")
	assert.Contains(t, data, "Subject: Tes")

	assert.Error(t, SMTPSender{Addr: s.Addr, From: "bukan email"}.Send(context.Background(), Message{To: "budi@example.com"}))
}

// fakeSMTP menjawab satu sesi SMTP minimal dan mengirim semua yang
// diterima ke got.
func fakeSMTP(t *testing.T, ln net.Listener, got chan<- string) {
	conn, err := ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	r := bufio.NewReader(conn)
	var log strings.Builder
	reply := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
	reply("220 fake ESMTP")
	inData := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			got <- log.String()
			return
		}
		log.WriteString(line)
		switch {
		case inData:
			if line == ".\r\n" {
				inData = false
				reply("250 OK")
			}
		case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
			reply("250 fake")
		case strings.HasPrefix(line, "DATA"):
			inData = true
			reply("354 go ahead")
		case strings.HasPrefix(line, "QUIT"):
			reply("221 bye")
			got <- log.String()
			return
		default:
			reply("250 OK")
		}
	}
}
