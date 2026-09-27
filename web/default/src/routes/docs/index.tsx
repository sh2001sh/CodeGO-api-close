import { createFileRoute } from '@tanstack/react-router'
import { ApiDocs } from '@/features/docs'

export const Route = createFileRoute('/docs/')({ component: ApiDocs })
