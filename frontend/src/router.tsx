import { createBrowserRouter } from 'react-router'
import { AuthLayout } from './components/AuthLayout'
import { Layout } from './components/Layout'
import { ErrorPage } from './pages/ErrorPage'
import { HomePage } from './pages/HomePage'
import { LoginPage } from './pages/LoginPage'
import { NotFoundPage } from './pages/NotFoundPage'
import { RegisterPage } from './pages/RegisterPage'
import { SearchPage } from './pages/SearchPage'
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
    ],
  },
])
