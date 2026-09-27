import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute('/_authenticated/group-buy/')({
  beforeLoad: () => {
    throw redirect({ to: '/packages', hash: 'group-buy', replace: true })
  },
})
