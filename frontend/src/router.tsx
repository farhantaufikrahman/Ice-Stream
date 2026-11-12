import { createBrowserRouter } from 'react-router'
import { AuthLayout } from './components/AuthLayout'
import { Layout } from './components/Layout'
import { RequireAuth } from './components/RequireAuth'
import { ChannelPage } from './pages/ChannelPage'
import { ErrorPage } from './pages/ErrorPage'
import { ForgotPasswordPage } from './pages/ForgotPasswordPage'
import { HistoryPage } from './pages/HistoryPage'
import { HomePage } from './pages/HomePage'
import { LivePage } from './pages/LivePage'
import { LiveWatchRoute } from './pages/LiveWatchPage'
import { LoginPage } from './pages/LoginPage'
import { NotFoundPage } from './pages/NotFoundPage'
import { NotificationsPage } from './pages/NotificationsPage'
import { OAuthDonePage } from './pages/OAuthDonePage'
import { PlaylistPage } from './pages/PlaylistPage'
import { RegisterPage } from './pages/RegisterPage'
import { ResetPasswordPage } from './pages/ResetPasswordPage'
import { SearchPage } from './pages/SearchPage'
import { SettingsPage } from './pages/SettingsPage'
import { StudioAnalyticsPage } from './pages/StudioAnalyticsPage'
import { StudioCommentsPage } from './pages/StudioCommentsPage'
import { StudioDanmakuPage } from './pages/StudioDanmakuPage'
import { StudioDashboardPage } from './pages/StudioDashboardPage'
import { StudioLivePage } from './pages/StudioLivePage'
import { StudioPage } from './pages/StudioPage'
import { TrendingPage } from './pages/TrendingPage'
import { UploadPage } from './pages/UploadPage'
import { WatchLaterPage } from './pages/WatchLaterPage'
import { WatchPage } from './pages/WatchPage'
import { InfoPage } from './pages/InfoPage'

// Daftar halaman. Mirip app.get('/watch/:id', ...) di Express, tetapi
// dijalankan di browser: berpindah halaman tidak memuat ulang seluruh situs.
// Dokumentasi: https://reactrouter.com/start/data/routing
export const router = createBrowserRouter([
  {
    errorElement: <ErrorPage />,
    children: [
      {
        // Halaman biasa: header + footer.
        element: <Layout />,
        children: [
          { path: '/', element: <HomePage /> },
          { path: '/search', element: <SearchPage /> },
          { path: '/trending', element: <TrendingPage /> },
          { path: '/watch/:id', element: <WatchPage /> },
          { path: '/channel/:username', element: <ChannelPage /> },
          // Siaran langsung (langkah 25c): daftar dan halaman tonton, tanpa login.
          { path: '/live', element: <LivePage /> },
          { path: '/live/:id', element: <LiveWatchRoute /> },
          // Boleh tanpa login: playlist PUBLIC bisa dibuka siapa saja.
          { path: '/playlist/:id', element: <PlaylistPage /> },
          // Halaman dari tautan footer (langkah 27).
          { path: '/tentang', element: <InfoPage topic="tentang" /> },
          { path: '/pedoman-komunitas', element: <InfoPage topic="pedoman" /> },
          { path: '/privasi', element: <InfoPage topic="privasi" /> },
          { path: '/bantuan', element: <InfoPage topic="bantuan" /> },
          {
            // Wajib login, tapi tetap memakai header dan footer biasa.
            element: <RequireAuth />,
            children: [
              { path: '/notifications', element: <NotificationsPage /> },
              { path: '/history', element: <HistoryPage /> },
              { path: '/watch-later', element: <WatchLaterPage /> },
            ],
          },
          { path: '*', element: <NotFoundPage /> },
        ],
      },
      {
        // Masuk, Daftar, dan Lupa kata sandi: panel brand di kiri, tanpa header.
        element: <AuthLayout />,
        children: [
          { path: '/login', element: <LoginPage /> },
          { path: '/register', element: <RegisterPage /> },
          // Lupa kata sandi (langkah 24c): tanpa login.
          { path: '/forgot-password', element: <ForgotPasswordPage /> },
          { path: '/reset-password', element: <ResetPasswordPage /> },
          // Akhir login Google/GitHub (langkah 26): menukar kode sekali pakai.
          { path: '/oauth/selesai', element: <OAuthDonePage /> },
        ],
      },
      {
        // Creator Studio: wajib login, punya kerangka sendiri (tanpa header utama).
        element: <RequireAuth />,
        children: [
          { path: '/studio', element: <StudioDashboardPage /> },
          { path: '/studio/content', element: <StudioPage /> },
          // Siaran langsung (langkah 25b).
          { path: '/studio/live', element: <StudioLivePage /> },
          // Analitik, Komentar, dan Danmaku (langkah 26).
          { path: '/studio/analytics', element: <StudioAnalyticsPage /> },
          { path: '/studio/comments', element: <StudioCommentsPage /> },
          { path: '/studio/danmaku', element: <StudioDanmakuPage /> },
          { path: '/studio/settings', element: <SettingsPage /> },
          { path: '/upload', element: <UploadPage /> },
        ],
      },
    ],
  },
])
