import { useState } from 'react'
import { useMutation, useSuspenseQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import type { Schema } from '../lib/types'
import { useTranslation } from '../lib/i18n'
import { credits } from '../lib/format'
import { DataTable } from './data-table'
import { Button, ErrorMessage } from './ui'

type Group = Schema['CatalogGroup']
type Price = Schema['CatalogPrice']
type Pool = Schema['CatalogRoutePool']
type CatalogRecord = Group | Price | Pool
type Editor =
  | { kind: 'groups'; value: Group }
  | { kind: 'prices'; value: Price }
  | { kind: 'route-pools'; value: Pool }
type Kind = Editor['kind']

function template(kind: Kind): Editor {
  if (kind === 'groups') return { kind, value: { name: '', description: '', multiplier: 1 } }
  if (kind === 'prices')
    return {
      kind,
      value: {
        model: '',
        mode: 'per_token',
        input_per_mtok: 0,
        output_per_mtok: 0,
        cache_read_per_mtok: 0,
        cache_write_per_mtok: 0,
        per_request: 0,
        rules: {},
      },
    }
  return {
    kind,
    value: {
      id: 0,
      group: 'default',
      model: '',
      strategy: 'weighted',
      enabled: true,
      members: [],
      name: '',
      model_scope: '',
      auto_discover: false,
      multiplier_weight: 1,
      ttft_weight: 1,
      cache_weight: 1,
      success_weight: 1,
    },
  }
}
function editor(row: CatalogRecord): Editor {
  if ('mode' in row) return { kind: 'prices', value: row }
  if ('id' in row) return { kind: 'route-pools', value: row }
  return { kind: 'groups', value: row }
}
function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}
function integer(value: unknown): value is number | string | bigint {
  return (
    typeof value === 'bigint' ||
    (typeof value === 'number' && Number.isSafeInteger(value)) ||
    (typeof value === 'string' && /^-?\d+$/.test(value))
  )
}
function group(value: unknown): value is Group {
  return (
    record(value) &&
    typeof value.name === 'string' &&
    typeof value.description === 'string' &&
    typeof value.multiplier === 'number'
  )
}
function price(value: unknown): value is Price {
  return (
    record(value) &&
    typeof value.model === 'string' &&
    typeof value.mode === 'string' &&
    record(value.rules) &&
    [
      'input_per_mtok',
      'output_per_mtok',
      'cache_read_per_mtok',
      'cache_write_per_mtok',
      'per_request',
    ].every((key) => integer(value[key]))
  )
}
function pool(value: unknown): value is Pool {
  return (
    record(value) &&
    integer(value.id) &&
    typeof value.group === 'string' &&
    typeof value.model === 'string' &&
    typeof value.strategy === 'string' &&
    typeof value.enabled === 'boolean' &&
    typeof value.name === 'string' &&
    typeof value.model_scope === 'string' &&
    typeof value.auto_discover === 'boolean' &&
    ['multiplier_weight', 'ttft_weight', 'cache_weight', 'success_weight'].every(
      (key) => typeof value[key] === 'number' && Number.isInteger(value[key]),
    ) &&
    (value.members === null ||
      (Array.isArray(value.members) &&
        value.members.every(
          (member) =>
            record(member) &&
            integer(member.channel_id) &&
            typeof member.priority === 'number' &&
            Number.isInteger(member.priority) &&
            typeof member.weight === 'number' &&
            Number.isInteger(member.weight) &&
            typeof member.cost_multiplier === 'number' &&
            Number.isFinite(member.cost_multiplier) &&
            typeof member.fault_domain === 'string' &&
            (member.model_cost_overrides === null ||
              (record(member.model_cost_overrides) &&
                Object.values(member.model_cost_overrides).every(
                  (cost) => typeof cost === 'number' && Number.isFinite(cost),
                ))),
        )))
  )
}
function parseEditor(kind: Kind, value: unknown): Editor {
  if (kind === 'groups' && group(value)) return { kind, value }
  if (kind === 'prices' && price(value)) return { kind, value }
  if (kind === 'route-pools' && pool(value)) return { kind, value }
  throw new Error('配置字段与当前类型不匹配')
}
function name(row: CatalogRecord): string {
  return 'id' in row ? row.name || String(row.id) : 'name' in row ? row.name : row.model
}

export function CatalogConfiguration() {
  const { t } = useTranslation()
  const [kind, setKind] = useState<Kind>('groups')
  const [editing, setEditing] = useState<Editor | null>(null)
  const [error, setError] = useState<Error | null>(null)
  const queryClient = useQueryClient()
  const fetchRecords = (signal: AbortSignal): Promise<CatalogRecord[]> => {
    if (kind === 'groups')
      return api.GET('/api/catalog/groups', { signal }).then((result) => unwrap(result))
    if (kind === 'prices')
      return api.GET('/api/catalog/prices', { signal }).then((result) => unwrap(result))
    return api.GET('/api/catalog/route-pools', { signal }).then((result) => unwrap(result))
  }
  const { data } = useSuspenseQuery(resourceOptions(`catalog-${kind}`, fetchRecords))
  const save = useMutation({
    mutationFn: (input: Editor) => {
      if (input.kind === 'groups')
        return api.PUT('/api/catalog/groups/{name}', {
          params: { path: { name: input.value.name } },
          body: input.value,
        })
      if (input.kind === 'prices')
        return api.PUT('/api/catalog/prices/{model}', {
          params: { path: { model: input.value.model } },
          body: input.value,
        })
      return api.PUT('/api/catalog/route-pools', { body: input.value })
    },
    onSuccess: () => {
      setEditing(null)
      void queryClient.invalidateQueries({ queryKey: [`catalog-${kind}`] })
    },
  })
  const remove = useMutation({
    mutationFn: (row: CatalogRecord) => {
      if ('mode' in row)
        return api.DELETE('/api/catalog/prices/{model}', { params: { path: { model: row.model } } })
      if ('id' in row)
        return api.DELETE('/api/catalog/route-pools/{id}', { params: { path: { id: row.id } } })
      return api.DELETE('/api/catalog/groups/{name}', { params: { path: { name: row.name } } })
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: [`catalog-${kind}`] }),
  })
  return (
    <section className="section">
      <div className="filters">
        <label className="field" htmlFor="catalog-kind">
          <span>{t('配置类型')}</span>
          <select
            id="catalog-kind"
            value={kind}
            onChange={(event) => {
              const value = event.target.value
              if (value === 'groups' || value === 'prices' || value === 'route-pools')
                setKind(value)
              setEditing(null)
            }}
          >
            <option value="groups">{t('分组')}</option>
            <option value="prices">{t('定价')}</option>
            <option value="route-pools">{t('路由池')}</option>
          </select>
        </label>
        <Button onClick={() => setEditing(template(kind))}>{t('创建')}</Button>
      </div>
      <ErrorMessage error={save.error ?? remove.error ?? error} />
      {editing && (
        <form
          className="form-panel"
          onSubmit={(event) => {
            event.preventDefault()
            setError(null)
            try {
              save.mutate(
                parseEditor(
                  editing.kind,
                  JSON.parse(String(new FormData(event.currentTarget).get('config'))),
                ),
              )
            } catch (failure) {
              setError(failure instanceof Error ? failure : new Error('配置值必须为有效 JSON'))
            }
          }}
        >
          <label className="field full-width" htmlFor="catalog-config">
            <span>{t('配置')}</span>
            <textarea
              id="catalog-config"
              name="config"
              rows={9}
              key={JSON.stringify(editing.value)}
              defaultValue={JSON.stringify(editing.value, null, 2)}
            />
          </label>
          <Button type="submit" disabled={save.isPending}>
            {t('保存')}
          </Button>
          <Button variant="quiet" type="button" onClick={() => setEditing(null)}>
            {t('取消')}
          </Button>
        </form>
      )}
      <DataTable
        rows={data}
        rowKey={(row) => ('id' in row ? row.id : name(row))}
        columns={[
          { label: '名称', render: name },
          {
            label: '模式',
            render: (row) => ('mode' in row ? row.mode : 'strategy' in row ? row.strategy : '—'),
          },
          {
            label: '倍率',
            render: (row) => ('multiplier' in row ? row.multiplier : '—'),
            numeric: true,
          },
          {
            label: '输入价 / 百万 token',
            render: (row) => credits('input_per_mtok' in row ? row.input_per_mtok : undefined),
            numeric: true,
          },
          {
            label: '输出价 / 百万 token',
            render: (row) => credits('output_per_mtok' in row ? row.output_per_mtok : undefined),
            numeric: true,
          },
          {
            label: '操作',
            render: (row) => (
              <div className="row-actions">
                <Button variant="quiet" onClick={() => setEditing(editor(row))}>
                  {t('编辑')}
                </Button>
                <Button
                  variant="danger"
                  disabled={remove.isPending}
                  onClick={() => {
                    if (window.confirm(t('删除后无法恢复，确认删除？'))) remove.mutate(row)
                  }}
                >
                  {t('删除')}
                </Button>
              </div>
            ),
          },
        ]}
      />
    </section>
  )
}
