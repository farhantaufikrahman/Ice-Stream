import { createBrowserRouter } from 'react-router'
import { AuthLayout } from './components/AuthLayout'
import { Layout } from './components/Layout'
import { RequireAuth } from './components/RequireAuth'
import { ChannelPage } from './pages/ChannelPage'
import { ErrorPage } from './pages/ErrorPage'
import { HistoryPage } from './pages/HistoryPage'
import { HomePage } from './pages/HomePage'
import { LoginPage } from './pages/LoginPage'
import { NotFoundPage } from './pages/NotFoundPage'
import { NotificationsPage } from './pages/NotificationsPage'
import { PlaylistPage } from './pages/PlaylistPage'
import { RegisterPage } from './pages/RegisterPage'
import { SearchPage } from './pages/SearchPage'
import { SettingsPage } from './pages/SettingsPage'
import { StudioDashboardPage } from './pages/StudioDashboardPage'
import { StudioPage } from './pages/StudioPage'
import { UploadPage } from './pages/UploadPage'
import { WatchLaterPage } from './pages/WatchLaterPage'
import { WatchPage } from './pages/WatchPage'

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
          { path: '/watch/:id', element: <WatchPage /> },
          { path: '/channel/:username', element: <ChannelPage /> },
          // Boleh tanpa login: playlist PUBLIC bisa dibuka siapa saja.
          { path: '/playlist/:id', element: <PlaylistPage /> },
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
        // Masuk dan Daftar: panel brand di kiri, tanpa header.
        element: <AuthLayout />,
        children: [
          { path: '/login', element: <LoginPage /> },
          { path: '/register', element: <RegisterPage /> },
        ],
      },
      {
        // Creator Studio: wajib login, punya kerangka sendiri (tanpa header utama).
        element: <RequireAuth />,
        children: [
          { path: '/studio', element: <StudioDashboardPage /> },
          { path: '/studio/content', element: <StudioPage /> },
          { path: '/studio/settings', element: <SettingsPage /> },
          { path: '/upload', element: <UploadPage /> },
        ],
      },
    ],
  },
])
