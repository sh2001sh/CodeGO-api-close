import { ChannelCreateDialog } from './channel-create-dialog'
import { OwnerChannels } from './owner-channels'
import { OwnerOperationsPanel } from './owner-operations-panel'

export function ChannelWorkspace(props: {
  showForm: boolean
  onShowForm: () => void
  onHideForm: () => void
}) {
  return (
    <>
      <OwnerChannels onAdd={props.onShowForm} />
      <OwnerOperationsPanel />
      <ChannelCreateDialog
        open={props.showForm}
        onOpenChange={(open) =>
          open ? props.onShowForm() : props.onHideForm()
        }
      />
    </>
  )
}
