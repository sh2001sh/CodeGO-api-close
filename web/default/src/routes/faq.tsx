import { createFileRoute } from '@tanstack/react-router'
import { FAQPage } from '@/features/faq'

export const Route = createFileRoute('/faq')({
  component: FAQPage,
})
