// Row detail drawer shared by the self and admin usage-log pages.
import { useTranslation } from '../../lib/i18n'
import { credits, date } from '../../lib/format'
import { Drawer, Status } from '../../components/ui'
import type { AuditUsage } from './types'
import { displayInt } from './format'

export function LogDetailDrawer(props: { usage: AuditUsage | null; onClose: () => void }) {
  const { t } = useTranslation()
  return (
    <Drawer
      open={props.usage !== null}
      onOpenChange={(open) => !open && props.onClose()}
      title="调用详情"
      description={props.usage?.request_id}
    >
      {props.usage && (
        <dl className="kv">
          <dt>{t('时间')}</dt>
          <dd>{date(props.usage.created_at)}</dd>
          <dt>{t('模型')}</dt>
          <dd>{props.usage.model}</dd>
          <dt>{t('终态')}</dt>
          <dd>
            <Status value={props.usage.terminal} />
          </dd>
          <dt>{t('扣费')}</dt>
          <dd>{credits(props.usage.amount)}</dd>
          <dt>{t('输入 token')}</dt>
          <dd>{displayInt(props.usage.prompt_tokens)}</dd>
          <dt>{t('输出 token')}</dt>
          <dd>{displayInt(props.usage.completion_tokens)}</dd>
          <dt>{t('缓存 token')}</dt>
          <dd>{displayInt(props.usage.cached_tokens)}</dd>
          <dt>{t('是否估算')}</dt>
          <dd>{props.usage.estimated ? t('是') : t('否')}</dd>
          <dt>{t('用户 ID')}</dt>
          <dd>{displayInt(props.usage.user_id)}</dd>
          <dt>{t('密钥 ID')}</dt>
          <dd>{displayInt(props.usage.key_id)}</dd>
          <dt>{t('渠道 ID')}</dt>
          <dd>{displayInt(props.usage.channel_id)}</dd>
          <dt>{t('请求 ID')}</dt>
          <dd className="mono">{props.usage.request_id}</dd>
        </dl>
      )}
    </Drawer>
  )
}
