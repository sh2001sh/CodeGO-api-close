import { createFileRoute } from '@tanstack/react-router'
import { SearchTopicsIndex } from '@/features/search-pages'

export const Route = createFileRoute('/topics/')({
  component: SearchTopicsIndex,
})
