import { createFileRoute } from '@tanstack/react-router'
import { SearchPage } from '@/features/search-pages'

export const Route = createFileRoute('/topics/$slug')({
  component: TopicPageRoute,
})

function TopicPageRoute() {
  const params = Route.useParams()
  return <SearchPage slug={params.slug} />
}
