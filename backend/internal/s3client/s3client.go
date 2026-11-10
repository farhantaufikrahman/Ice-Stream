// Package s3client membuat client AWS SDK for Go v2 untuk storage yang
// kompatibel S3 (RustFS). Dipakai bersama oleh VideoService dan transcoder,
// supaya pengaturannya (path-style, checksum) selalu sama.
package s3client

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

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