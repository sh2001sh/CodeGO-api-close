import { createFileRoute } from '@tanstack/react-router'
import { PackagesPage } from '@/features/packages'

export const Route = createFileRoute('/_authenticated/packages/')({
  component: PackagesPage,
})
