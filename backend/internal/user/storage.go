package user

import (
	"bytes"
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/farhantaufikrahman/ice-stream/backend/internal/config"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/s3client"
)

// AvatarPrefix adalah folder foto profil di bucket images. Hanya folder ini
// yang boleh dibaca siapa saja tanpa login.
const AvatarPrefix = "avatars/"

// S3Store menyimpan foto profil di bucket images (RustFS).
type S3Store struct {
	bucket string
	client *s3.Client
}

// NewS3Store membuat S3Store. Belum ada koneksi yang dibuka di sini.
func NewS3Store(cfg config.StorageConfig) *S3Store {
	return &S3Store{bucket: cfg.Bucket, client: s3client.New(cfg, cfg.Endpoint)}
}

var _ ObjectStore = (*S3Store)(nil)

// EnsureBucket membuat bucket jika belum ada dan memasang bucket policy
// "baca publik" untuk AvatarPrefix. CORS tidak perlu: browser menampilkan
// foto lewat <img>, dan upload lewat API gateway, bukan langsung ke bucket.
func (s *S3Store) EnsureBucket(ctx context.Context) error {
	if err := s3client.EnsureBucket(ctx, s.client, s.bucket); err != nil {
		return err
	}
	return s3client.PutPublicReadPolicy(ctx, s.client, s.bucket, AvatarPrefix)
}

// Ping memeriksa apakah storage bisa dihubungi dan bucket-nya ada.
func (s *S3Store) Ping(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	return err
}

// Put menyimpan file. Nama file foto profil selalu baru (acak) setiap kali
// diganti, jadi browser boleh menyimpannya di cache selamanya (immutable).
func (s *S3Store) Put(ctx context.Context, key, contentType string, data []byte) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(data),
		ContentLength: aws.Int64(int64(len(data))),
		ContentType:   aws.String(contentType),
		CacheControl:  aws.String("public, max-age=31536000, immutable"),
	})
	if err != nil {
		return fmt.Errorf("upload %s: %w", key, err)
	}
	return nil
}

// Delete menghapus file. Menghapus file yang tidak ada bukan error (aturan
// API S3).
func (s *S3Store) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return fmt.Errorf("hapus %s: %w", key, err)
	}
	return nil
}
