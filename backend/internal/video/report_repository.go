package video

import (
	"context"
	"fmt"
)

// saveReportSQL: satu laporan per (video, pelapor). Jika sudah ada, isinya
// diganti dan statusnya dibuka lagi. Padanannya di PostgreSQL adalah
// INSERT ... ON CONFLICT (video_id, reporter_id) DO UPDATE, di MySQL
// INSERT ... ON DUPLICATE KEY UPDATE.
//
// Keterangan kosong tersimpan sebagai NULL tanpa kode tambahan, karena di
// Oracle string kosong ” sama dengan NULL. (Di PostgreSQL dan MySQL
// keduanya berbeda.)
const saveReportSQL = `MERGE INTO video_reports r
USING (SELECT ? AS video_id, ? AS reporter_id FROM dual) s
   ON (r.video_id = s.video_id AND r.reporter_id = s.reporter_id)
 WHEN MATCHED THEN UPDATE SET
   r.reason = ?, r.details = ?, r.status = 'OPEN', r.reported_at = ?
 WHEN NOT MATCHED THEN
   INSERT (video_id, reporter_id, reason, details, status, reported_at)
   VALUES (s.video_id, s.reporter_id, ?, ?, 'OPEN', ?)`

// SaveReport menyimpan laporan dengan satu MERGE.
//
// Dua laporan pertama dari user yang sama yang datang bersamaan bisa
// sama-sama mencoba INSERT, dan salah satunya gagal dengan ORA-00001.
// Yang menang sudah menyimpan laporan user itu, jadi error ini diabaikan.
func (r *OracleRepository) SaveReport(ctx context.Context, rep Report) error {
	reason := string(rep.Reason)
	err := r.db.WithContext(ctx).Exec(saveReportSQL,
		rep.VideoID, rep.ReporterID,
		reason, rep.Details, rep.ReportedAt,
		reason, rep.Details, rep.ReportedAt,
	).Error
	if err != nil && !isUniqueViolation(err) {
		return fmt.Errorf("merge video_reports: %w", err)
	}
	return nil
}
