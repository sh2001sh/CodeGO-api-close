import { Loader2, PackageOpen } from 'lucide-react'
import { cn } from '@/lib/utils'

export function EditorialBoxGlyph(props: {
  pending: boolean
  opening: boolean
}) {
  return (
    <div
      className={cn('blind-box-optic', props.pending && 'has-inventory')}
      aria-hidden='true'
    >
      <span className='blind-box-optic-orbit' />
      {props.opening ? (
        <Loader2 className='animate-spin' strokeWidth={1.2} />
      ) : (
        <PackageOpen strokeWidth={1.2} />
      )}
    </div>
  )
}
