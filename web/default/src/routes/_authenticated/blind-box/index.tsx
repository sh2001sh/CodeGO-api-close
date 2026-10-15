import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { BlindBoxPage } from '@/features/wallet/blind-box-page'

const blindBoxSearchSchema = z.object({
  pay: z.enum(['success', 'pending', 'fail']).optional(),
})

export const Route = createFileRoute('/_authenticated/blind-box/')({
  component: RouteComponent,
  validateSearch: blindBoxSearchSchema,
})

function RouteComponent() {
  const { pay } = Route.useSearch()
  return <BlindBoxPage initialPaymentStatus={pay} />
}
