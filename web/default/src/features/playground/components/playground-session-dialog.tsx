/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog'
import type { PlaygroundSession } from '../types'

export type SessionDialogState =
  | { action: 'rename' | 'delete'; session: PlaygroundSession }
  | { action: 'clear' | 'import' }

interface PlaygroundSessionDialogProps {
  state: SessionDialogState | null
  name: string
  onNameChange: (name: string) => void
  onClose: () => void
  onConfirm: () => void
  count: number
}

export function PlaygroundSessionDialog(props: PlaygroundSessionDialogProps) {
  const { t } = useTranslation()
  const action = props.state?.action
  let title = t('Rename')
  let consequence = ''
  if (action === 'delete') {
    title = t('删除本地会话')
    consequence = t('删除此会话及消息')
  }
  if (action === 'clear') {
    title = t('清空本地')
    consequence = `${props.count} ${t('会话')} · ${t('仅清除当前账号的本地会话')}`
  }
  if (action === 'import') {
    title = t('导入旧会话')
    consequence = t('将此设备的旧会话归入当前账号')
  }
  return (
    <Dialog
      open={Boolean(props.state)}
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
    >
      <DialogContent className='pg-session-dialog sm:max-w-md'>
        <DialogTitle>{title}</DialogTitle>
        {consequence && <DialogDescription>{consequence}</DialogDescription>}
        <form
          onSubmit={(event) => {
            event.preventDefault()
            props.onConfirm()
          }}
          className='grid gap-5'
        >
          {action === 'rename' && (
            <div className='pg-field'>
              <label htmlFor='pg-session-name'>{t('会话名称')}</label>
              <input
                id='pg-session-name'
                value={props.name}
                onChange={(event) => props.onNameChange(event.target.value)}
                autoFocus
                maxLength={120}
                required
              />
            </div>
          )}
          {props.state && 'session' in props.state && action === 'delete' && (
            <p className='break-words'>
              {props.state.session.title || t('新建会话')}
            </p>
          )}
          <div className='flex justify-end gap-2'>
            <Button type='button' variant='outline' onClick={props.onClose}>
              {t('Cancel')}
            </Button>
            <Button
              type='submit'
              variant={
                action === 'delete' || action === 'clear'
                  ? 'destructive'
                  : 'default'
              }
              disabled={action === 'rename' && !props.name.trim()}
            >
              {action === 'rename' ? t('Save') : t('Confirm')}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}
