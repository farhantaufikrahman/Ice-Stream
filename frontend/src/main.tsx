import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { RouterProvider } from 'react-router/dom'
import { AuthProvider } from './auth/AuthProvider'
import './index.css'
import { UnreadProvider } from './notifications/UnreadProvider'
import { router } from './router'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <AuthProvider>
      <UnreadProvider>
        <RouterProvider router={router} />
      </UnreadProvider>
    </AuthProvider>
  </StrictMode>,
)
