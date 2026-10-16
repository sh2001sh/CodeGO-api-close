import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute('/_authenticated/daily-lucky-number/')({
  beforeLoad: () => {
    throw redirect({ to: '/wallet', replace: true })
  },
})
