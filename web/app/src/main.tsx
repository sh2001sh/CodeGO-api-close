import { StrictMode, Suspense } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from '@tanstack/react-router'
import { router, queryClient } from './router'
import { LanguageProvider } from './lib/i18n'
import { Loading } from './components/ui'
import './styles.css'

const root = document.getElementById('root')
if (!root) throw new Error('Root element is missing')
createRoot(root).render(
  <StrictMode>
    <LanguageProvider>
      <QueryClientProvider client={queryClient}>
        <Suspense fallback={<Loading />}>
          <RouterProvider router={router} />
        </Suspense>
      </QueryClientProvider>
    </LanguageProvider>
  </StrictMode>,
)
