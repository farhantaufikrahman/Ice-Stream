package video

// Upload multipart (langkah 29).
//
// Cloudflare paket gratis menolak request di atas 100 MB, padahal video
// boleh sampai 2 GB. Jadi file dikirim browser per bagian (part) dengan
// fitur "multipart upload" S3:
//
//  1. CreateUpload  -> CreateMultipartUpload, upload_id disimpan di tabel videos.
//  2. Browser minta URL presigned per bagian (PresignUploadParts), lalu
//     PUT tiap bagian langsung ke storage. Bagian yang gagal cukup dikirim
//     ulang; bagian yang sudah tersimpan tidak perlu dikirim lagi.
//  3. CompleteUpload -> ListParts + CompleteMultipartUpload: storage
//     menggabungkan semua bagian menjadi satu file raw/<acak>/source.
//
// Daftar bagian (beserta ETag-nya) dibaca server sendiri dari storage
// (ListParts), bukan dari browser, jadi browser tidak perlu membaca header
// ETag dan tidak bisa mengirim daftar palsu.
//
// Dokumentasi: https://docs.aws.amazon.com/AmazonS3/latest/userguide/mpuoverview.html
// Padanannya: @aws-sdk/lib-storage Upload (Node.js), S3TransferManager
// (Java), feature/s3/manager.Uploader (Go). Bedanya, di sini yang
// memotong file adalah browser, karena file tidak pernah lewat server.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

const (
	// DefaultUploadPartBytes adalah ukuran tiap bagian: 50 MB, jauh di bawah
	// batas 100 MB Cloudflare dan di atas batas minimal S3 (5 MB, kecuali
	// bagian terakhir). Video 2 GB = 41 bagian.
	DefaultUploadPartBytes int64 = 50 << 20
	// maxPartsPerPresign membatasi jumlah URL dalam satu PresignUploadParts.
	maxPartsPerPresign = 20
)

// MultipartStore adalah bagian ObjectStore untuk multipart upload.
type MultipartStore interface {
	// CreateMultipart memulai multipart upload dan mengembalikan upload id.
	CreateMultipart(ctx context.Context, key, contentType string) (string, error)
	// PresignUploadPart membuat URL PUT untuk satu bagian. size ikut
	// ditandatangani, jadi bagian dengan ukuran lain ditolak storage.
	PresignUploadPart(ctx context.Context, key, uploadID string, partNumber int32, size int64, ttl time.Duration) (PresignedRequest, error)
	// ListParts mendaftar bagian yang sudah tersimpan, urut dari nomor
	// terkecil. ErrUploadNotFound jika upload id tidak ada lagi (sudah
	// digabung atau dibatalkan).
	ListParts(ctx context.Context, key, uploadID string) ([]UploadedPart, error)
	// CompleteMultipart menggabungkan bagian-bagian menjadi satu file.
	CompleteMultipart(ctx context.Context, key, uploadID string, parts []UploadedPart) error
	// AbortMultipart membatalkan upload dan menghapus semua bagiannya.
	// Upload yang sudah tidak ada bukan error.
	AbortMultipart(ctx context.Context, key, uploadID string) error
}

// UploadedPart adalah satu bagian yang sudah tersimpan di storage.
type UploadedPart struct {
	Number int32
	Size   int64
	ETag   string
}

// ErrUploadNotFound dikembalikan ListParts jika upload id tidak dikenal.
var ErrUploadNotFound = errors.New("multipart upload tidak ditemukan")

// partPlan menghitung jumlah bagian untuk file berukuran size.
func partPlan(size, partSize int64) int32 {
	return int32((size + partSize - 1) / partSize)
}

// partSizeOf adalah ukuran bagian ke-n (mulai 1): semua partSize kecuali
// bagian terakhir yang berisi sisanya.
func partSizeOf(n int32, size, partSize int64) int64 {
	start := int64(n-1) * partSize
	if rest := size - start; rest < partSize {
		return rest
	}
	return partSize
}

// UploadParts adalah hasil PresignUploadParts.
type UploadParts struct {
	Parts     []PartRequest
	ExpiresAt time.Time
	// Uploaded adalah nomor bagian yang sudah tersimpan lengkap.
	Uploaded []int32
}

// PartRequest adalah URL presigned untuk satu bagian.
type PartRequest struct {
	Number  int32
	Size    int64
	Request PresignedRequest
}

func (s *Service) partBytes() int64 {
	if s.opts.UploadPartBytes > 0 {
		return s.opts.UploadPartBytes
	}
	return DefaultUploadPartBytes
}

// PresignUploadParts memberi URL untuk bagian yang diminta, dan daftar
// bagian yang sudah tersimpan (supaya browser bisa melanjutkan upload).
func (s *Service) PresignUploadParts(ctx context.Context, ownerID, videoID int64, numbers []int32) (UploadParts, error) {
	v, err := s.repo.FindByID(ctx, videoID)
	if err != nil {
		return UploadParts{}, err
	}
	if v.OwnerID != ownerID {
		return UploadParts{}, ErrNotFound
	}
	if v.Status != StatusPendingUpload || v.UploadID == "" {
		return UploadParts{}, ErrNotUploading
	}
	partSize := s.partBytes()
	count := partPlan(v.SizeBytes, partSize)
	if len(numbers) > maxPartsPerPresign {
		return UploadParts{}, &ValidationError{"part_numbers", fmt.Sprintf("maksimal %d bagian sekaligus", maxPartsPerPresign)}
	}
	seen := make(map[int32]bool, len(numbers))
	for _, n := range numbers {
		if n < 1 || n > count {
			return UploadParts{}, &ValidationError{"part_numbers", fmt.Sprintf("nomor bagian harus 1 sampai %d", count)}
		}
		seen[n] = true
	}

	stored, err := s.store.ListParts(ctx, v.ObjectKey, v.UploadID)
	if err != nil {
		return UploadParts{}, err
	}
	out := UploadParts{ExpiresAt: s.now().Add(s.opts.UploadURLTTL), Uploaded: []int32{}}
	for _, p := range stored {
		// Bagian dengan ukuran salah (tidak mungkin lewat URL presigned,
		// tapi jangan percaya begitu saja) dianggap belum terkirim.
		if p.Number >= 1 && p.Number <= count && p.Size == partSizeOf(p.Number, v.SizeBytes, partSize) {
			out.Uploaded = append(out.Uploaded, p.Number)
		}
	}

	wanted := make([]int32, 0, len(seen))
	for n := range seen {
		wanted = append(wanted, n)
	}
	sort.Slice(wanted, func(i, j int) bool { return wanted[i] < wanted[j] })
	for _, n := range wanted {
		size := partSizeOf(n, v.SizeBytes, partSize)
		req, err := s.store.PresignUploadPart(ctx, v.ObjectKey, v.UploadID, n, size, s.opts.UploadURLTTL)
		if err != nil {
			return UploadParts{}, err
		}
		out.Parts = append(out.Parts, PartRequest{Number: n, Size: size, Request: req})
	}
	return out, nil
}

// finishMultipart menggabungkan semua bagian jika lengkap. Tidak melakukan
// apa-apa jika upload id sudah tidak ada (sudah digabung oleh panggilan
// sebelumnya; CompleteUpload lalu memeriksa file hasilnya dengan Stat).
func (s *Service) finishMultipart(ctx context.Context, v Video) error {
	parts, err := s.store.ListParts(ctx, v.ObjectKey, v.UploadID)
	if errors.Is(err, ErrUploadNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	partSize := s.partBytes()
	count := partPlan(v.SizeBytes, partSize)
	if int32(len(parts)) != count {
		return ErrUploadIncomplete
	}
	for i, p := range parts {
		n := int32(i + 1)
		if p.Number != n || p.Size != partSizeOf(n, v.SizeBytes, partSize) {
			return ErrUploadIncomplete
		}
	}
	return s.store.CompleteMultipart(ctx, v.ObjectKey, v.UploadID, parts)
}

// ---------- S3Store ----------

func (s *S3Store) CreateMultipart(ctx context.Context, key, contentType string) (string, error) {
	out, err := s.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("create multipart upload: %w", err)
	}
	return aws.ToString(out.UploadId), nil
}

func (s *S3Store) PresignUploadPart(ctx context.Context, key, uploadID string, partNumber int32, size int64, ttl time.Duration) (PresignedRequest, error) {
	req, err := s.presigner.PresignUploadPart(ctx, &s3.UploadPartInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		UploadId:      aws.String(uploadID),
		PartNumber:    aws.Int32(partNumber),
		ContentLength: aws.Int64(size),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return PresignedRequest{}, fmt.Errorf("presign upload part: %w", err)
	}
	return toPresigned(req, ttl), nil
}

func (s *S3Store) ListParts(ctx context.Context, key, uploadID string) ([]UploadedPart, error) {
	var parts []UploadedPart
	p := s3.NewListPartsPaginator(s.client, &s3.ListPartsInput{
		Bucket:   aws.String(s.bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			if isNoSuchUpload(err) {
				return nil, ErrUploadNotFound
			}
			return nil, fmt.Errorf("list parts: %w", err)
		}
		for _, part := range page.Parts {
			parts = append(parts, UploadedPart{
				Number: aws.ToInt32(part.PartNumber),
				Size:   aws.ToInt64(part.Size),
				ETag:   aws.ToString(part.ETag),
			})
		}
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].Number < parts[j].Number })
	return parts, nil
}

func (s *S3Store) CompleteMultipart(ctx context.Context, key, uploadID string, parts []UploadedPart) error {
	completed := make([]types.CompletedPart, len(parts))
	for i, p := range parts {
		completed[i] = types.CompletedPart{PartNumber: aws.Int32(p.Number), ETag: aws.String(p.ETag)}
	}
	_, err := s.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:          aws.String(s.bucket),
		Key:             aws.String(key),
		UploadId:        aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{Parts: completed},
	})
	if err != nil {
		return fmt.Errorf("complete multipart upload: %w", err)
	}
	return nil
}

func (s *S3Store) AbortMultipart(ctx context.Context, key, uploadID string) error {
	_, err := s.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(s.bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	})
	if err != nil && !isNoSuchUpload(err) {
		return fmt.Errorf("abort multipart upload: %w", err)
	}
	return nil
}

func isNoSuchUpload(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == "NoSuchUpload"
}
