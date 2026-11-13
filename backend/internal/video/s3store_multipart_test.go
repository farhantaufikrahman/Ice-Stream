package video

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/farhantaufikrahman/ice-stream/backend/internal/config"
)

// fakeS3 meniru sebagian kecil API multipart S3 (cukup untuk S3Store),
// supaya kode S3Store bisa dites tanpa RustFS sungguhan. Bentuk XML-nya
// mengikuti dokumentasi Amazon S3:
// https://docs.aws.amazon.com/AmazonS3/latest/API/API_ListParts.html
//
// Perbandingan: di Node.js biasanya memakai aws-sdk-client-mock, di Java
// memakai LocalStack/Testcontainers. Di Go, httptest.Server sudah cukup
// karena SDK bisa diarahkan ke alamat mana pun (BaseEndpoint).
type fakeS3 struct {
	mu        sync.Mutex
	completed []int32 // nomor bagian yang dikirim saat Complete
	aborted   []string
	failNext  bool
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q := r.URL.Query()
	uploadID := q.Get("uploadId")

	if f.failNext {
		f.failNext = false
		// 403 tidak dicoba ulang oleh SDK (berbeda dengan 500/503).
		writeS3Error(w, http.StatusForbidden, "AccessDenied")
		return
	}
	if uploadID == "missing" {
		writeS3Error(w, http.StatusNotFound, "NoSuchUpload")
		return
	}

	switch {
	case r.Method == http.MethodPost && q.Has("uploads"):
		_, _ = io.WriteString(w, `<InitiateMultipartUploadResult><Bucket>videos</Bucket><Key>raw/1/a.mp4</Key><UploadId>up-1</UploadId></InitiateMultipartUploadResult>`)
	case r.Method == http.MethodGet && uploadID != "":
		// Dua halaman, urutan sengaja diacak: S3Store harus menggabung
		// semua halaman lalu mengurutkan berdasarkan nomor bagian.
		if q.Get("part-number-marker") == "" {
			_, _ = io.WriteString(w, `<ListPartsResult><IsTruncated>true</IsTruncated><NextPartNumberMarker>3</NextPartNumberMarker>`+
				`<Part><PartNumber>3</PartNumber><Size>5</Size><ETag>"e3"</ETag></Part>`+
				`<Part><PartNumber>1</PartNumber><Size>10</Size><ETag>"e1"</ETag></Part></ListPartsResult>`)
			return
		}
		_, _ = io.WriteString(w, `<ListPartsResult><IsTruncated>false</IsTruncated>`+
			`<Part><PartNumber>2</PartNumber><Size>10</Size><ETag>"e2"</ETag></Part></ListPartsResult>`)
	case r.Method == http.MethodPost && uploadID != "":
		var body struct {
			Parts []struct {
				PartNumber int32 `xml:"PartNumber"`
			} `xml:"Part"`
		}
		if err := xml.NewDecoder(r.Body).Decode(&body); err != nil {
			writeS3Error(w, http.StatusBadRequest, "MalformedXML")
			return
		}
		for _, p := range body.Parts {
			f.completed = append(f.completed, p.PartNumber)
		}
		_, _ = io.WriteString(w, `<CompleteMultipartUploadResult><Bucket>videos</Bucket><Key>raw/1/a.mp4</Key><ETag>"all"</ETag></CompleteMultipartUploadResult>`)
	case r.Method == http.MethodDelete && uploadID != "":
		f.aborted = append(f.aborted, uploadID)
		w.WriteHeader(http.StatusNoContent)
	default:
		writeS3Error(w, http.StatusNotImplemented, "NotImplemented")
	}
}

func writeS3Error(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, `<Error><Code>`+code+`</Code><Message>fake</Message></Error>`)
}

func newFakeS3Store(t *testing.T) (*S3Store, *fakeS3) {
	t.Helper()
	fake := &fakeS3{}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	store := NewS3Store(config.StorageConfig{
		Endpoint:       srv.URL,
		PublicEndpoint: "https://upload.example.test",
		Region:         "us-east-1",
		AccessKey:      "test",
		SecretKey:      "test-secret",
		Bucket:         "videos",
	})
	return store, fake
}

func TestS3StoreMultipartFlow(t *testing.T) {
	store, fake := newFakeS3Store(t)
	ctx := context.Background()

	id, err := store.CreateMultipart(ctx, "raw/1/a.mp4", "video/mp4")
	require.NoError(t, err)
	assert.Equal(t, "up-1", id)

	parts, err := store.ListParts(ctx, "raw/1/a.mp4", id)
	require.NoError(t, err)
	require.Len(t, parts, 3)
	assert.Equal(t, []int32{1, 2, 3}, []int32{parts[0].Number, parts[1].Number, parts[2].Number})
	assert.Equal(t, int64(5), parts[2].Size)
	assert.Equal(t, `"e1"`, parts[0].ETag)

	require.NoError(t, store.CompleteMultipart(ctx, "raw/1/a.mp4", id, parts))
	assert.Equal(t, []int32{1, 2, 3}, fake.completed)

	require.NoError(t, store.AbortMultipart(ctx, "raw/1/a.mp4", id))
	assert.Equal(t, []string{"up-1"}, fake.aborted)
}

func TestS3StoreMultipartNoSuchUpload(t *testing.T) {
	store, _ := newFakeS3Store(t)
	ctx := context.Background()

	_, err := store.ListParts(ctx, "raw/1/a.mp4", "missing")
	assert.ErrorIs(t, err, ErrUploadNotFound)

	// Membatalkan upload yang sudah tidak ada bukan error (aman diulang).
	assert.NoError(t, store.AbortMultipart(ctx, "raw/1/a.mp4", "missing"))
}

func TestS3StoreMultipartStorageErrors(t *testing.T) {
	store, fake := newFakeS3Store(t)
	ctx := context.Background()

	fake.failNext = true
	_, err := store.CreateMultipart(ctx, "raw/1/a.mp4", "video/mp4")
	assert.ErrorContains(t, err, "create multipart upload")

	fake.failNext = true
	_, err = store.ListParts(ctx, "raw/1/a.mp4", "up-1")
	assert.ErrorContains(t, err, "list parts")

	fake.failNext = true
	err = store.CompleteMultipart(ctx, "raw/1/a.mp4", "up-1", []UploadedPart{{Number: 1, ETag: `"e1"`}})
	assert.ErrorContains(t, err, "complete multipart upload")

	fake.failNext = true
	err = store.AbortMultipart(ctx, "raw/1/a.mp4", "up-1")
	assert.ErrorContains(t, err, "abort multipart upload")
}

func TestS3StorePresignUploadPart(t *testing.T) {
	store, _ := newFakeS3Store(t)

	before := time.Now()
	req, err := store.PresignUploadPart(context.Background(), "raw/1/a.mp4", "up-1", 2, 10, 15*time.Minute)
	require.NoError(t, err)

	u, err := url.Parse(req.URL)
	require.NoError(t, err)
	// Alamat publik (yang dibuka browser), bukan alamat di dalam cluster.
	assert.Equal(t, "upload.example.test", u.Host)
	assert.True(t, strings.HasPrefix(u.Path, "/videos/raw/1/a.mp4"))
	assert.Equal(t, "2", u.Query().Get("partNumber"))
	assert.Equal(t, "up-1", u.Query().Get("uploadId"))
	// Ukuran bagian ikut ditandatangani, jadi browser tidak bisa mengirim
	// bagian yang lebih besar dari rencana.
	assert.Contains(t, u.Query().Get("X-Amz-SignedHeaders"), "content-length")
	assert.NotContains(t, req.Headers, "Content-Length")
	assert.WithinDuration(t, before.Add(15*time.Minute), req.ExpiresAt, 5*time.Second)
}
