import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute('/brand')({
  beforeLoad: () => {
    throw redirect({ to: '/' })
  },
})
