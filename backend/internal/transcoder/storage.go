package transcoder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/farhantaufikrahman/ice-stream/backend/internal/config"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/s3client"
)

// Storage adalah semua yang dibutuhkan worker dari object storage.
type Storage interface {
	// Download menyimpan object key ke file dst.
	Download(ctx context.Context, key, dst string) error
	// UploadDir mengunggah semua file di dir ke prefix (misalnya "hls/42/"),
	// dengan nama relatif yang sama.
	UploadDir(ctx context.Context, dir, prefix string) error
}

// ErrSourceMissing berarti file mentah tidak ada di storage.
var ErrSourceMissing = errors.New("file mentah tidak ditemukan di storage")

// S3Storage mengakses RustFS (atau S3 lain) memakai AWS SDK for Go v2.
type S3Storage struct {
	bucket string
	client *s3.Client
}

// NewS3Storage membuat S3Storage. Belum ada koneksi yang dibuka di sini.
func NewS3Storage(cfg config.StorageConfig) *S3Storage {
	return &S3Storage{bucket: cfg.Bucket, client: s3client.New(cfg, cfg.Endpoint)}
}

var _ Storage = (*S3Storage)(nil)

// Ping memeriksa apakah storage bisa dihubungi dan bucket-nya ada.
func (s *S3Storage) Ping(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	return err
}

func (s *S3Storage) Download(ctx context.Context, key, dst string) error {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		var nsk *types.NoSuchKey
		if errors.As(err, &nsk) {
			return fmt.Errorf("%w: %s", ErrSourceMissing, key)
		}
		return fmt.Errorf("get object %s: %w", key, err)
	}
	defer out.Body.Close()

	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	// Disalin sedikit demi sedikit (streaming), jadi file 2 GB tidak perlu
	// dimuat seluruhnya ke RAM.
	if _, err := io.Copy(f, out.Body); err != nil {
		f.Close()
		return fmt.Errorf("download %s: %w", key, err)
	}
	return f.Close()
}

func (s *S3Storage) UploadDir(ctx context.Context, dir, prefix string) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		key := prefix + filepath.ToSlash(rel)
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
			Bucket:      aws.String(s.bucket),
			Key:         aws.String(key),
			Body:        f,
			ContentType: aws.String(contentType(path)),
		})
		if err != nil {
			return fmt.Errorf("upload %s: %w", key, err)
		}
		return nil
	})
}

// contentType menentukan header Content-Type dari ekstensi file. Player
// HLS di browser memeriksa header ini.
func contentType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".m3u8":
		return "application/vnd.apple.mpegurl"
	case ".ts":
		return "video/mp2t"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	}
	return "application/octet-stream"
}