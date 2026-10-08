import { useState, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import { useTranslation } from '../../lib/i18n'
import { currentPolicyVersion, hasCurrentPolicyAcceptance } from '../../lib/legal-policy'
import { Button, ErrorMessage, Loading, Panel } from '../../components/ui'

// Wrap only new submissions/publication actions. Existing channels remain manageable.
export function SupplierAgreementGate(props: { children: ReactNode; required?: boolean }) {
  const { t, locale } = useTranslation()
  const client = useQueryClient()
  const [checked, setChecked] = useState(false)
  const required = props.required !== false
  const records = useQuery({
    queryKey: ['policy-acceptance'],
    queryFn: async () => unwrap(await api.GET('/api/user/policy-acceptance')),
    enabled: required,
  })
  const accept = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST('/api/user/policy-acceptance', {
          body: { document: 'supplier', version: currentPolicyVersion, locale },
        }),
      ),
    onSuccess: (record) => {
      client.setQueryData(
        ['policy-acceptance'],
        [
          ...(records.data ?? []).filter(
            (item) => !(item.document === record.document && item.version === record.version),
          ),
          record,
        ],
      )
      void client.invalidateQueries({ queryKey: ['policy-acceptance'] })
    },
  })
  if (!required || (records.data && hasCurrentPolicyAcceptance(records.data, 'supplier')))
    return props.children
  if (records.isPending) return <Loading rows={2} />
  return (
    <Panel title="发布渠道前">
      <p>{t('请先阅读渠道供给与结算协议，了解来源授权、数据处理、费用与违规处置规则。')}</p>
      <p>
        <a href="/supplier-agreement" target="_blank" rel="noopener noreferrer">
          {t('渠道供给与结算协议')}
        </a>
        {' · '}
        {t('版本')} {currentPolicyVersion}
      </p>
      <ErrorMessage error={records.error ?? accept.error} />
      {records.isError ? (
        <Button variant="quiet" onClick={() => void records.refetch()}>
          {t('重新加载')}
        </Button>
      ) : (
        <form
          className="form-stack"
          onSubmit={(event) => {
            event.preventDefault()
            if (checked) accept.mutate()
          }}
        >
          <label className="checkbox-field">
            <input
              type="checkbox"
              required
              checked={checked}
              onChange={(event) => setChecked(event.target.checked)}
            />
            <span>{t('我已阅读并同意渠道供给与结算协议。')}</span>
          </label>
          <Button type="submit" disabled={!checked || accept.isPending}>
            {t(accept.isPending ? '正在提交' : '同意并继续')}
          </Button>
        </form>
      )}
    </Panel>
  )
}
