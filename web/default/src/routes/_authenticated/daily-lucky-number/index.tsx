import { createFileRoute } from '@tanstack/react-router'
import { DailyLuckyNumberPage } from '@/features/daily-lucky-number'

export const Route = createFileRoute('/_authenticated/daily-lucky-number/')({
  component: DailyLuckyNumberPage,
})
