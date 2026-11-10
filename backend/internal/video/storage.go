package video

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/farhantaufikrahman/ice-stream/backend/internal/config"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/s3client"
)

// ObjectStore adalah semua yang dibutuhkan Service dari object storage.
// Dengan interface ini, test bisa memakai storage palsu, dan RustFS bisa
// diganti AWS S3 atau Cloudflare R2 tanpa mengubah Service.
type ObjectStore interface {
	// PresignPut membuat URL untuk mengunggah satu file. contentType dan size
	// ikut ditandatangani, jadi upload dengan tipe atau ukuran lain ditolak.
	PresignPut(ctx context.Context, key, contentType string, size int64, ttl time.Duration) (PresignedRequest, error)
	// PresignGet membuat URL untuk mengunduh/memutar satu file.
	PresignGet(ctx context.Context, key string, ttl time.Duration) (PresignedRequest, error)
	// Stat membaca ukuran dan tipe file. ErrObjectNotFound jika file tidak ada.
	Stat(ctx context.Context, key string) (ObjectInfo, error)
}

// PresignedRequest adalah URL presigned beserta header yang wajib dikirim.
type PresignedRequest struct {
	URL       string
	Headers   map[string]string
	ExpiresAt time.Time
}

// ObjectInfo adalah metadata file di storage.
type ObjectInfo struct {
	Size        int64
	ContentType string
}

// ErrObjectNotFound dikembalikan Stat jika file belum ada.
var ErrObjectNotFound = errors.New("object tidak ditemukan")

// S3Store mengakses storage yang kompatibel S3 memakai AWS SDK for Go v2.
// Padanannya: @aws-sdk/client-s3 di Node.js, software.amazon.awssdk:s3 di Java.
type S3Store struct {
	bucket string
	// client dipakai untuk memanggil storage dari dalam cluster.
	client *s3.Client
	// presigner membuat URL dengan alamat publik (yang bisa dibuka browser).
	presigner *s3.PresignClient
}

// NewS3Store membuat S3Store. Belum ada koneksi yang dibuka di sini.
func NewS3Store(cfg config.StorageConfig) *S3Store {
	return &S3Store{
		bucket:    cfg.Bucket,
		client:    s3client.New(cfg, cfg.Endpoint),
		presigner: s3.NewPresignClient(s3client.New(cfg, cfg.PublicEndpoint)),
	}
}

var _ ObjectStore = (*S3Store)(nil)

// PublicPrefix adalah folder di bucket yang boleh dibaca siapa saja tanpa
// login: hasil transcoding (HLS dan thumbnail) yang ditulis transcoder ke
// hls/<id>/. File mentah di raw/ tetap privat (hanya lewat URL presigned).
const PublicPrefix = "hls/"

// EnsureBucket membuat bucket jika belum ada, lalu memasang:
//   - aturan CORS, supaya browser dari origin yang diizinkan boleh mengunggah
//     langsung dan memutar HLS;
//   - bucket policy, supaya file di PublicPrefix bisa dibaca tanpa tanda
//     tangan. Player HLS mengambil puluhan file kecil (.m3u8 dan .ts), jadi
//     URL presigned per file tidak praktis. Cara ini sama dengan bucket S3
//     publik di belakang CDN (YouTube, Netflix, dan lainnya).
//
// Aman dipanggil setiap kali service start.
func (s *S3Store) EnsureBucket(ctx context.Context, allowedOrigins []string) error {
	_, err := s.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(s.bucket)})
	var owned *types.BucketAlreadyOwnedByYou
	var exists *types.BucketAlreadyExists
	if err != nil && !errors.As(err, &owned) && !errors.As(err, &exists) {
		return fmt.Errorf("membuat bucket %q: %w", s.bucket, err)
	}
	_, err = s.client.PutBucketCors(ctx, &s3.PutBucketCorsInput{
		Bucket: aws.String(s.bucket),
		CORSConfiguration: &types.CORSConfiguration{CORSRules: []types.CORSRule{{
			AllowedOrigins: allowedOrigins,
			AllowedMethods: []string{http.MethodGet, http.MethodHead, http.MethodPut},
			AllowedHeaders: []string{"*"},
			// ETag dibaca frontend nanti untuk multipart upload.
			ExposeHeaders: []string{"ETag"},
			MaxAgeSeconds: aws.Int32(3600),
		}}},
	})
	if err != nil {
		return fmt.Errorf("memasang CORS bucket %q: %w", s.bucket, err)
	}
	policy, err := publicReadPolicy(s.bucket, PublicPrefix)
	if err != nil {
		return err
	}
	_, err = s.client.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
		Bucket: aws.String(s.bucket),
		Policy: aws.String(policy),
	})
	if err != nil {
		return fmt.Errorf("memasang bucket policy %q: %w", s.bucket, err)
	}
	return nil
}

// publicReadPolicy membuat bucket policy (format IAM, sama dengan AWS S3)
// yang mengizinkan siapa saja (Principal "*") membaca file di bawah prefix.
// Hanya s3:GetObject: daftar isi bucket, mengunggah, dan menghapus tetap
// butuh kunci.
func publicReadPolicy(bucket, prefix string) (string, error) {
	type statement struct {
		Sid       string              `json:"Sid"`
		Effect    string              `json:"Effect"`
		Principal map[string][]string `json:"Principal"`
		Action    []string            `json:"Action"`
		Resource  []string            `json:"Resource"`
	}
	doc := struct {
		Version   string      `json:"Version"`
		Statement []statement `json:"Statement"`
	}{
		Version: "2012-10-17",
		Statement: []statement{{
			Sid:       "PublicReadHLS",
			Effect:    "Allow",
			Principal: map[string][]string{"AWS": {"*"}},
			Action:    []string{"s3:GetObject"},
			Resource:  []string{"arn:aws:s3:::" + bucket + "/" + prefix + "*"},
		}},
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("membuat bucket policy: %w", err)
	}
	return string(b), nil
}

// Ping memeriksa apakah storage bisa dihubungi dan bucket-nya ada.
func (s *S3Store) Ping(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	return err
}

func (s *S3Store) PresignPut(ctx context.Context, key, contentType string, size int64, ttl time.Duration) (PresignedRequest, error) {
	req, err := s.presigner.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(size),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return PresignedRequest{}, fmt.Errorf("presign put: %w", err)
	}
	return toPresigned(req, ttl), nil
}

func (s *S3Store) PresignGet(ctx context.Context, key string, ttl time.Duration) (PresignedRequest, error) {
	req, err := s.presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return PresignedRequest{}, fmt.Errorf("presign get: %w", err)
	}
	return toPresigned(req, ttl), nil
}

func (s *S3Store) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		// HEAD tidak punya body, jadi "tidak ada" hanya terlihat dari kode 404.
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NotFound" || apiErr.ErrorCode() == "NoSuchKey") {
			return ObjectInfo{}, ErrObjectNotFound
		}
		return ObjectInfo{}, fmt.Errorf("head object: %w", err)
	}
	return ObjectInfo{
		Size:        aws.ToInt64(out.ContentLength),
		ContentType: aws.ToString(out.ContentType),
	}, nil
}

// toPresigned mengambil header yang ikut ditandatangani. Host dan
// Content-Length dibuang karena browser selalu mengisinya sendiri (dan
// JavaScript tidak boleh mengubahnya). Nilainya tetap dicek storage.
func toPresigned(req *v4.PresignedHTTPRequest, ttl time.Duration) PresignedRequest {
	headers := make(map[string]string, len(req.SignedHeader))
	for k, v := range req.SignedHeader {
		k = http.CanonicalHeaderKey(k)
		if k == "Host" || k == "Content-Length" || len(v) == 0 {
			continue
		}
		headers[k] = v[0]
	}
	return PresignedRequest{
		URL:       req.URL,
		Headers:   headers,
		ExpiresAt: time.Now().Add(ttl),
	}
}
