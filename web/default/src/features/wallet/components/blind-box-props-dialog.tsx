import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import type { BlindBoxProp } from '../types'
import { BlindBoxPropsList } from './blind-box-view-parts'

export function BlindBoxPropsDialog(props: {
  open: boolean
  props: BlindBoxProp[]
  disabled: boolean
  convertingPropId: number | null
  onOpenChange: (open: boolean) => void
  onUse: (prop: BlindBoxProp) => void
  onPause: (prop: BlindBoxProp) => void
  onConvert: (prop: BlindBoxProp) => void
  onGift: (prop: BlindBoxProp) => void
}) {
  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='max-h-[calc(100dvh-2rem)] overflow-hidden sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>我的道具</DialogTitle>
        </DialogHeader>
        <div className='max-h-[calc(100dvh-10rem)] overflow-y-auto pr-1'>
          <BlindBoxPropsList
            props={props.props}
            disabled={props.disabled}
            onUse={props.onUse}
            onPause={props.onPause}
            onConvert={props.onConvert}
            onGift={props.onGift}
            convertingPropId={props.convertingPropId}
          />
        </div>
      </DialogContent>
    </Dialog>
  )
}
