import { useState } from 'react'
import { Button, Dialog, Tabs, CopyButton } from '../../components/ui'
import { useTranslation } from '../../lib/i18n'
import { curlSample, pythonSample } from './code-sample'
import type { ChatMessage, PlaygroundSettings } from './types'

export function CodeDialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  history: ChatMessage[]
  settings: PlaygroundSettings
}) {
  const { t } = useTranslation()
  const [tab, setTab] = useState('curl')
  const origin = window.location.origin
  const curl = curlSample(origin, props.history, props.settings)
  const python = pythonSample(origin, props.history, props.settings)
  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title="查看代码"
      size="lg"
      footer={
        <Button variant="secondary" onClick={() => props.onOpenChange(false)}>
          {t('关闭')}
        </Button>
      }
    >
      <Tabs
        value={tab}
        onValueChange={setTab}
        items={[
          {
            value: 'curl',
            label: 'curl',
            content: (
              <div className="code-block">
                <CopyButton value={curl} label="复制示例" />
                <pre>
                  <code>{curl}</code>
                </pre>
              </div>
            ),
          },
          {
            value: 'python',
            label: 'Python',
            content: (
              <div className="code-block">
                <CopyButton value={python} label="复制示例" />
                <pre>
                  <code>{python}</code>
                </pre>
              </div>
            ),
          },
        ]}
      />
    </Dialog>
  )
}
