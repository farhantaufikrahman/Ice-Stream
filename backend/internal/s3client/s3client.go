// Package s3client membuat client AWS SDK for Go v2 untuk storage yang
// kompatibel S3 (RustFS), beserta fungsi persiapan bucket. Dipakai bersama
// oleh VideoService, transcoder, dan UserService, supaya pengaturannya
// (path-style, checksum, bucket policy) selalu sama.
package s3client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/farhantaufikrahman/ice-stream/backend/internal/config"
)

// New membuat client S3 yang memanggil endpoint (misalnya
// "http://storage:9000"). Belum ada koneksi yang dibuka di sini.
func New(cfg config.StorageConfig, endpoint string) *s3.Client {
	return s3.New(s3.Options{
		BaseEndpoint: aws.String(endpoint),
		Region:       cfg.Region,
		Credentials:  credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		// Path-style: http://host:9000/videos/key, bukan
		// http://videos.host:9000/key. Storage self-hosted seperti RustFS
		// tidak punya DNS per bucket.
		UsePathStyle: true,
		// Jangan tambahkan checksum CRC32 otomatis. Kalau ditambahkan,
		// browser juga harus menghitung checksum saat mengunggah.
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	})
}

// EnsureBucket membuat bucket jika belum ada. Aman dipanggil setiap kali
// service start.
func EnsureBucket(ctx context.Context, c *s3.Client, bucket string) error {
	_, err := c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	var owned *types.BucketAlreadyOwnedByYou
	var exists *types.BucketAlreadyExists
	if err != nil && !errors.As(err, &owned) && !errors.As(err, &exists) {
		return fmt.Errorf("membuat bucket %q: %w", bucket, err)
	}
	return nil
}

// PutPublicReadPolicy memasang bucket policy yang membuat file di bawah
// prefix bisa dibaca siapa saja tanpa tanda tangan (lihat PublicReadPolicy).
// Policy lama di bucket yang sama diganti.
func PutPublicReadPolicy(ctx context.Context, c *s3.Client, bucket, prefix string) error {
	policy, err := PublicReadPolicy(bucket, prefix)
	if err != nil {
		return err
	}
	_, err = c.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
		Bucket: aws.String(bucket),
		Policy: aws.String(policy),
	})
	if err != nil {
		return fmt.Errorf("memasang bucket policy %q: %w", bucket, err)
	}
	return nil
}

// PublicReadPolicy membuat bucket policy (format IAM, sama dengan AWS S3)
// yang mengizinkan siapa saja (Principal "*") membaca file di bawah prefix.
// Hanya s3:GetObject: daftar isi bucket, mengunggah, dan menghapus tetap
// butuh kunci.
func PublicReadPolicy(bucket, prefix string) (string, error) {
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
			Sid:       "PublicRead",
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
