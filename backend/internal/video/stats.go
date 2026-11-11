package video

import (
	"context"
	"time"
)

// Statistik channel untuk dasbor Creator Studio (langkah 22b).
//
// Satu "hari" di dasbor adalah satu tanggal kalender WIB. Indonesia tidak
// memakai daylight saving time, jadi zona waktu tetap UTC+7 sudah tepat
// sepanjang tahun dan tidak perlu database zona waktu (tzdata) di image
// Docker.
var statsZone = time.FixedZone("WIB", 7*60*60)

// statsDateLayout adalah format tanggal di tabel video_daily_views dan di
// jawaban API: "2006-01-02" (lihat dokumentasi paket time).
const statsDateLayout = time.DateOnly

// allowedStatsDays adalah pilihan rentang di dasbor (desain Studio:
// "7 hari", "28 hari", "90 hari").
var allowedStatsDays = map[int]bool{7: true, 28: true, 90: true}

// statsDay mengubah waktu menjadi tanggal WIB, misalnya 2026-11-10T18:30Z
// menjadi "2026-11-11" (01:30 WIB).
func statsDay(t time.Time) string {
	return t.In(statsZone).Format(statsDateLayout)
}

// ChannelTotals adalah jumlah dalam satu periode.
type ChannelTotals struct {
	Views    int64
	Danmaku  int64
	Comments int64
}

// DailyViews adalah tayangan semua video satu channel pada satu tanggal.
type DailyViews struct {
	Date  string
	Views int64
}

// ChannelStats adalah isi dasbor channel.
//
// Periode ini = [CurrentStart, End), periode sebelumnya =
// [PreviousStart, CurrentStart). End adalah tengah malam WIB setelah hari
// ini, jadi hari ini ikut dihitung walau belum selesai.
type ChannelStats struct {
	Daily         []DailyViews
	Current       ChannelTotals
	Previous      ChannelTotals
	PreviousStart time.Time
	CurrentStart  time.Time
	End           time.Time
}

// statsWindow menghitung batas periode untuk days hari yang berakhir hari
// ini (WIB). Hasilnya dalam UTC.
func statsWindow(now time.Time, days int) (previousStart, currentStart, end time.Time) {
	local := now.In(statsZone)
	// Tengah malam WIB setelah hari ini. time.Date menormalkan hari ke-32
	// dan seterusnya ke bulan berikutnya.
	end = time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, statsZone)
	currentStart = end.AddDate(0, 0, -days)
	previousStart = currentStart.AddDate(0, 0, -days)
	return previousStart.UTC(), currentStart.UTC(), end.UTC()
}

// GetChannelStats mengambil statistik semua video milik ownerID untuk days
// hari terakhir (termasuk hari ini) dan days hari sebelumnya.
//
// Video yang sudah dihapus tetap dihitung: tayangan, danmaku, dan komentar
// yang diterima saat video itu masih ada tetap bagian dari sejarah channel
// (YouTube Studio juga begitu). Komentar yang dihapus tidak dihitung.
func (s *Service) GetChannelStats(ctx context.Context, ownerID int64, days int) (ChannelStats, error) {
	if ownerID <= 0 {
		return ChannelStats{}, ErrNotFound
	}
	if !allowedStatsDays[days] {
		return ChannelStats{}, &ValidationError{"days", "harus 7, 28, atau 90"}
	}
	previousStart, currentStart, end := statsWindow(s.now(), days)
	// Tanggal terakhir = sehari sebelum end.
	lastDay := statsDay(end.Add(-time.Nanosecond))

	perDay, err := s.repo.ChannelDailyViews(ctx, ownerID, statsDay(previousStart), lastDay)
	if err != nil {
		return ChannelStats{}, err
	}
	current, previous, err := s.repo.ChannelActivity(ctx, ownerID, previousStart, currentStart, end)
	if err != nil {
		return ChannelStats{}, err
	}

	out := ChannelStats{
		Daily:         make([]DailyViews, 0, days),
		Current:       current,
		Previous:      previous,
		PreviousStart: previousStart,
		CurrentStart:  currentStart,
		End:           end,
	}
	// ChannelActivity tidak mengisi Views. Tayangan dijumlahkan dari data
	// per hari, jadi total di kartu sama persis dengan jumlah batang grafik.
	for d := previousStart.In(statsZone); d.Before(end); d = d.AddDate(0, 0, 1) {
		day := d.Format(statsDateLayout)
		views := perDay[day]
		if d.Before(currentStart) {
			out.Previous.Views += views
			continue
		}
		out.Current.Views += views
		out.Daily = append(out.Daily, DailyViews{Date: day, Views: views})
	}
	return out, nil
}
