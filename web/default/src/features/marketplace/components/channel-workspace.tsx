import { ChannelCreateDialog } from './channel-create-dialog'
import { OwnerChannels } from './owner-channels'

export function ChannelWorkspace(props: {
  showForm: boolean
  onShowForm: () => void
  onHideForm: () => void
}) {
  return (
    <>
      <OwnerChannels onAdd={props.onShowForm} />
      <ChannelCreateDialog
        open={props.showForm}
        onOpenChange={(open) =>
          open ? props.onShowForm() : props.onHideForm()
        }
      />
    </>
  )
}
