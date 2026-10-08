import { useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import { resourceOptions } from '../../lib/queries'
import { useToast } from '../../hooks/use-toast'
import { useTranslation } from '../../lib/i18n'
import { credits, toMicroCredits } from '../../lib/format'
import type { Schema } from '../../lib/types'
import { DataTable } from '../../components/data-table'
import { Button, ErrorMessage, Field, Loading, Status } from '../../components/ui'
import { decimalCredits } from '../commerce/amounts'
import { newPoolRewards, newStandardPolicy, parsePoolConfig, poolConfigJSON } from './pool-draft'
import { PoolRewardFields } from './pool-reward-fields'
import '../blind-box/blind-box.css'

type Pool = Schema['MarketplacePool']

export const blindBoxPoolsOptions = () =>
  resourceOptions('blind-box-pools', (signal) =>
    api.GET('/api/blind-box/admin/pools', { signal }).then(unwrap),
  )

export function PoolEditor() {
  const { t } = useTranslation()
  const toast = useToast((s) => s.add)
  const client = useQueryClient()
  const pools = useQuery(blindBoxPoolsOptions())
  const [editing, setEditing] = useState<Pool | 'new' | null>(null)
  const [draftError, setDraftError] = useState<Error | null>(null)
  const submitting = useRef(false)
  const save = useMutation({
    mutationFn: (body: Schema['MarketplacePoolInput']) =>
      api.PUT('/api/blind-box/admin/pools', { body }).then(unwrap),
    onSuccess: () => {
      toast(t('已保存'), 'success')
      setEditing(null)
      void client.invalidateQueries({ queryKey: ['blind-box-pools'] })
      void client.invalidateQueries({ queryKey: ['boxes'] })
    },
    onError: (error: Error) => toast(error.message, 'error'),
    onSettled: () => {
      submitting.current = false
    },
  })
  const current = editing && editing !== 'new' ? editing : undefined
  return (
    <section className="section">
      <div className="page-header">
        <h2>{t('盲盒池配置')}</h2>
        <Button
          disabled={save.isPending}
          onClick={() => {
            save.reset()
            setDraftError(null)
            setEditing('new')
          }}
        >
          {t('新建盲盒池')}
        </Button>
      </div>
      <p className="muted">
        {t('此列表包含启用和停用的奖池。编辑会保留完整奖励与保底规则，停用的奖池可重新启用。')}
      </p>
      <ErrorMessage error={draftError ?? pools.error ?? save.error} />
      {pools.isPending && <Loading />}
      <DataTable
        rows={pools.data ?? []}
        rowKey={(row) => String(row.id)}
        empty="暂无盲盒池"
        columns={[
          { label: 'ID', render: (row) => String(row.id) },
          { label: '名称', render: (row) => row.name },
          { label: '范围', render: (row) => row.scope },
          { label: '单价', render: (row) => credits(row.price_micro), numeric: true },
          { label: '每日限购', render: (row) => String(row.daily_limit) },
          {
            label: '状态',
            render: (row) => <Status value={row.enabled ? 'enabled' : 'disabled'} />,
          },
          {
            label: '操作',
            render: (row) => (
              <Button
                variant="quiet"
                disabled={save.isPending}
                onClick={() => {
                  save.reset()
                  setDraftError(null)
                  setEditing(row)
                }}
              >
                {t('编辑')}
              </Button>
            ),
          },
        ]}
      />
      {editing && (
        <form
          key={String(current?.id ?? 'new')}
          className="form-panel"
          onSubmit={(event) => {
            event.preventDefault()
            if (submitting.current) return
            const form = new FormData(event.currentTarget)
            const text = (name: string) => String(form.get(name) ?? '').trim()
            try {
              const config = parsePoolConfig(
                text('pool-rewards'),
                text('pool-guarantees'),
                text('pool-standard-policy'),
                text('pool-scope'),
              )
              const price = BigInt(toMicroCredits(text('pool-price')))
              if (price <= 0n) throw new Error('盲盒价格必须大于零')
              setDraftError(null)
              submitting.current = true
              save.mutate({
                ...current,
                id: current?.id,
                name: text('pool-name'),
                enabled: form.has('pool-enabled'),
                price_micro: price,
                daily_limit: Number(text('pool-daily-limit')),
                monthly_limit: Number(text('pool-monthly-limit') || '0'),
                daily_open_limit: Number(text('pool-daily-open-limit') || '0'),
                scope: text('pool-scope'),
                ...config,
              })
            } catch (error) {
              submitting.current = false
              setDraftError(
                error instanceof Error ? error : new Error('配置 JSON 格式无效，请检查后重试'),
              )
            }
          }}
        >
          <Field
            name="pool-name"
            label="池名称"
            required
            maxLength={200}
            defaultValue={current?.name}
          />
          <label className="field" htmlFor="pool-scope">
            <span>{t('范围')}</span>
            <select id="pool-scope" name="pool-scope" defaultValue={current?.scope ?? 'credits'}>
              <option value="credits">{t('credits（兼容购买）')}</option>
              <option value="standard">{t('standard（新版标准池）')}</option>
            </select>
          </label>
          <Field
            name="pool-price"
            label="单个购买价格 credits"
            required
            defaultValue={current ? decimalCredits(current.price_micro) : '1'}
          />
          <Field
            name="pool-daily-limit"
            label="每日限购次数"
            type="number"
            min={1}
            max={10000}
            required
            defaultValue={current ? Number(current.daily_limit) : 10}
          />
          <Field
            name="pool-monthly-limit"
            label="每月限购次数（0 为不限）"
            type="number"
            min={0}
            defaultValue={Number(current?.monthly_limit ?? 0)}
          />
          <Field
            name="pool-daily-open-limit"
            label="每日开启次数（0 为不限）"
            type="number"
            min={0}
            defaultValue={Number(current?.daily_open_limit ?? 0)}
          />
          <label className="checkbox-field">
            <input type="checkbox" name="pool-enabled" defaultChecked={current?.enabled ?? true} />
            {t('启用此池')}
          </label>
          <p className="muted full-width">
            {t(
              '高级配置使用完整 JSON。金额单位为 micro-credits；权重保持精确整数。修改前请核对原有奖励、金额区间、道具上限和保底规则。',
            )}
          </p>
          <PoolRewardFields initial={current?.rewards ?? newPoolRewards} />
          <label className="field full-width" htmlFor="pool-guarantees">
            <span>{t('保底配置 JSON')}</span>
            <textarea
              id="pool-guarantees"
              name="pool-guarantees"
              rows={10}
              required
              spellCheck={false}
              defaultValue={poolConfigJSON(current?.guarantees ?? {})}
            />
          </label>
          <label className="field full-width" htmlFor="pool-standard-policy">
            <span>{t('标准池策略 JSON')}</span>
            <textarea
              id="pool-standard-policy"
              name="pool-standard-policy"
              rows={10}
              required
              spellCheck={false}
              defaultValue={poolConfigJSON(current?.standard_policy ?? newStandardPolicy)}
            />
          </label>
          <Button type="submit" disabled={save.isPending}>
            {t('保存盲盒池')}
          </Button>
          <Button
            variant="quiet"
            type="button"
            disabled={save.isPending}
            onClick={() => {
              setDraftError(null)
              setEditing(null)
            }}
          >
            {t('取消')}
          </Button>
        </form>
      )}
    </section>
  )
}
