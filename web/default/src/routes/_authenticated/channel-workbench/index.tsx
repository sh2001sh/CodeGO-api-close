import { createFileRoute } from '@tanstack/react-router'
import { ChannelWorkbench } from '@/features/marketplace/channel-workbench'

export const Route = createFileRoute('/_authenticated/channel-workbench/')({
  component: ChannelWorkbench,
})
