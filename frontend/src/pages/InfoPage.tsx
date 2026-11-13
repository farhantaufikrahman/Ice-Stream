import type { ReactNode } from 'react'
import { Link } from 'react-router'
import { INFO_LINKS, type InfoTopic } from '../lib/info'

/**
 * Halaman informasi dari tautan footer (desain Home.dc.html: Tentang,
 * Pedoman komunitas, Privasi, Bantuan), langkah 27.
 *
 * Isinya hanya menjelaskan apa yang benar-benar dilakukan aplikasi ini
 * (angka dan aturan diambil dari kode backend), bukan janji yang belum ada
 * fiturnya. Jika aturannya berubah di backend, ubah juga teks di sini.
 */
export function InfoPage({ topic }: { topic: InfoTopic }) {
  const page = PAGES[topic]
  return (
    <article className="info-page">
      <h1 className="page-title">{page.title}</h1>
      {page.body}
      <nav className="info-nav" aria-label="Halaman informasi lain">
        {INFO_LINKS.filter((l) => l.topic !== topic).map((l) => (
          <Link key={l.topic} to={l.path}>
            {l.label}
          </Link>
        ))}
      </nav>
    </article>
  )
}

const PAGES: Record<InfoTopic, { title: string; body: ReactNode }> = {
  tentang: {
    title: 'Tentang Ice-Stream',
    body: (
      <>
        <p>
          Ice-Stream adalah tempat berbagi video dan siaran langsung dengan <strong>danmaku</strong>: komentar singkat yang melayang di atas
          video, tepat di detik saat komentar itu ditulis. Menonton jadi terasa seperti menonton bersama.
        </p>
        <h2>Yang bisa kamu lakukan</h2>
        <ul>
          <li>Menonton video tanpa akun, lalu masuk untuk menyukai, berkomentar, mengirim danmaku, dan mengikuti kanal.</li>
          <li>Menyimpan video ke Tonton nanti dan playlist, dan melanjutkan tontonan dari riwayat.</li>
          <li>Mengunggah video. Video diproses otomatis menjadi beberapa resolusi supaya lancar di koneksi apa pun.</li>
          <li>Siaran langsung dari browser (kamera atau layar) atau dari aplikasi seperti OBS, dengan chat yang juga tampil sebagai danmaku.</li>
          <li>Melihat statistik kanal, komentar, dan danmaku di Creator Studio.</li>
        </ul>
        <p>
          Butuh petunjuk? Buka <Link to="/bantuan">Bantuan</Link>.
        </p>
      </>
    ),
  },
  pedoman: {
    title: 'Pedoman komunitas',
    body: (
      <>
        <p>Ice-Stream terbuka untuk semua orang. Supaya tetap nyaman, ikuti aturan berikut di video, judul, komentar, danmaku, dan chat siaran.</p>
        <h2>Yang tidak boleh</h2>
        <ul>
          <li>Spam atau penipuan, termasuk tautan yang menyesatkan dan pesan yang diulang-ulang.</li>
          <li>Konten seksual.</li>
          <li>Kekerasan atau konten berbahaya.</li>
          <li>Ujaran kebencian terhadap siapa pun.</li>
          <li>Perundungan atau pelecehan terhadap orang lain.</li>
          <li>Informasi yang menyesatkan.</li>
          <li>Mengunggah karya orang lain tanpa izin (pelanggaran hak cipta).</li>
        </ul>
        <h2>Batas yang berlaku otomatis</h2>
        <ul>
          <li>Danmaku dan pesan chat siaran: satu baris, maksimal 100 karakter.</li>
          <li>Komentar: maksimal 1.000 karakter.</li>
          <li>Kreator bisa menyalakan mode lambat (satu pesan per 3 detik) dan chat khusus pengikut di siarannya.</li>
          <li>Kreator bisa menghapus komentar dan danmaku di videonya dari Creator Studio.</li>
        </ul>
        <h2>Melaporkan</h2>
        <p>
          Tekan <strong>Laporkan</strong> di bawah video, pilih alasannya, dan tambahkan keterangan jika perlu. Setiap akun punya satu laporan
          per video; melapor lagi memperbarui laporan sebelumnya. Laporan disimpan untuk ditinjau.
        </p>
        <p>
          Admin meninjau laporan. Video yang melanggar diturunkan: video itu hilang dari beranda, pencarian, kanal, dan tautannya, lalu
          pemiliknya mendapat notifikasi beserta alasannya. Jika laporannya tidak terbukti, laporan ditolak dan video tetap tayang.
        </p>
      </>
    ),
  },
  privasi: {
    title: 'Privasi',
    body: (
      <>
        <p>Halaman ini menjelaskan data apa yang disimpan Ice-Stream dan untuk apa.</p>
        <h2>Akun</h2>
        <ul>
          <li>Username, email, nama tampilan, bio, dan foto profil. Email tidak pernah ditampilkan kepada orang lain.</li>
          <li>Kata sandi tidak disimpan apa adanya, hanya hasil hash bcrypt yang tidak bisa dibalik.</li>
          <li>Jika masuk dengan Google atau GitHub, yang disimpan adalah id akunmu di sana dan email terverifikasinya.</li>
        </ul>
        <h2>Aktivitas</h2>
        <ul>
          <li>Riwayat tonton (video dan posisi terakhir), supaya kamu bisa melanjutkan menonton. Kamu bisa menghapusnya di halaman Riwayat.</li>
          <li>Suka, komentar, danmaku, chat siaran, playlist, Tonton nanti, dan kanal yang kamu ikuti.</li>
          <li>
            Rekomendasi di Beranda disusun dari riwayat tonton dan kanal yang kamu ikuti. Tanpa login, rekomendasi hanya memakai video populer dan
            terbaru.
          </li>
          <li>
            Untuk menghitung tayangan tanpa menghitung dua kali, penonton yang belum login dikenali dari sidik acak (hash) alamat IP dan jenis
            browser. Sidik ini hanya disimpan 30 menit.
          </li>
        </ul>
        <h2>Siaran langsung</h2>
        <p>
          Server siaran merekam setiap siaran sementara dan menghapusnya otomatis setelah 1 hari. Rekaman hanya disimpan menjadi video (draf yang
          hanya bisa dilihat kreatornya sampai diterbitkan) jika kreator mencentang <em>Simpan rekaman sebagai video setelah siaran selesai</em>.
        </p>
        <h2>Cookie dan penyimpanan browser</h2>
        <ul>
          <li>Cookie login (berlaku 30 hari) supaya kamu tidak perlu masuk lagi setiap membuka situs. Cookie ini dihapus saat kamu keluar.</li>
          <li>Pilihan tema, danmaku, dan putar otomatis disimpan di browsermu sendiri, tidak dikirim ke server.</li>
        </ul>
        <p>Menghapus akun belum bisa dilakukan dari aplikasi.</p>
      </>
    ),
  },
  bantuan: {
    title: 'Bantuan',
    body: (
      <>
        <h2>Mengunggah video</h2>
        <p>
          Tekan <strong>Unggah</strong> di kanan atas, pilih file (MP4, WebM, MOV, atau MKV, maksimal 2 GB), isi judul, lalu pilih{' '}
          <strong>Terbitkan</strong> atau <strong>Simpan draf</strong>. Setelah terunggah, video berstatus <em>Antre</em> lalu <em>Diproses</em>{' '}
          beberapa menit sebelum bisa ditonton.
        </p>
        <h2>Danmaku</h2>
        <p>
          Tulis danmaku di bawah player saat video diputar; danmaku muncul di detik itu untuk semua penonton. Tombol <strong>Danmaku aktif</strong>{' '}
          menyembunyikan atau menampilkannya.
        </p>
        <h2>Siaran langsung</h2>
        <p>
          Buka Creator Studio, menu <strong>Siaran langsung</strong>. Pilih <em>Kamera browser</em> atau <em>Bagikan layar</em> lalu tekan{' '}
          <strong>Mulai siaran</strong>, atau pilih <em>Aplikasi (OBS)</em> dan salin URL server serta kunci stream ke OBS. Di panel{' '}
          <em>Detail siaran</em> kamu bisa menyimpan rekaman sebagai video, menyalakan mode lambat, dan membatasi chat untuk pengikut.
        </p>
        <h2>Video unggulan</h2>
        <p>
          Pilih video yang ditampilkan besar di halaman kanalmu di Creator Studio, menu <strong>Pengaturan</strong>. Hanya video publik yang
          sudah terbit yang bisa dipilih.
        </p>
        <h2>Lupa kata sandi</h2>
        <p>
          Tekan <Link to="/forgot-password">Lupa kata sandi?</Link> di halaman Masuk. Tautan untuk membuat kata sandi baru dikirim ke emailmu dan
          berlaku 30 menit.
        </p>
        <h2>Melihat konten yang melanggar</h2>
        <p>
          Laporkan lewat tombol <strong>Laporkan</strong> di bawah video. Aturannya ada di <Link to="/pedoman-komunitas">Pedoman komunitas</Link>.
        </p>
      </>
    ),
  },
}
