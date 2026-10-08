import { useState } from 'react'
import { useTranslation } from '../../lib/i18n'
import type { Schema } from '../../lib/types'
import { Button, Field } from '../../components/ui'
import { MarketForm, text } from './form'
import {
  boundedInteger,
  moveMember,
  orderedMembers,
  poolAutoBuild,
  poolInput,
  poolStrategy,
  validateAutoBuild,
  type Pool,
} from './pool-settings'

export function PoolEditor(props: {
  pool?: Pool
  automatic: boolean
  groups: readonly Schema['ChannelMarketRoutePoolGroupOption'][]
  pending: boolean
  onSave: (input: Schema['ChannelMarketRoutePoolInput']) => void
  onCancel: () => void
}) {
  const { t } = useTranslation()
  const initial = poolAutoBuild(props.pool)
  const [memberIDs, setMemberIDs] = useState(() =>
    [...(props.pool?.members ?? [])]
      .sort((a, b) => a.priority - b.priority)
      .map((member) => member.group_id),
  )
  const [autoEnabled, setAutoEnabled] = useState(initial.enabled)
  const [models, setModels] = useState(initial.models)
  const [schedule, setSchedule] = useState(initial.schedule)
  const availableModels = [
    ...new Set([...props.groups.flatMap((group) => group.models), ...models]),
  ].sort()
  const groupName = (id: string) => {
    const group = props.groups.find((item) => item.group_id === id)
    return group
      ? `${group.kind === 'official' ? `${t('官方分组')} · ` : ''}${group.name} · ${group.display_id}`
      : id.startsWith('official:')
        ? `${t('官方分组')} · ${id.slice('official:'.length)}`
        : t('未列出的分组')
  }
  return (
    <MarketForm
      pending={props.pending}
      onSubmit={(fields) => {
        const maximumText = text(fields, 'maximum') || '0'
        if (!/^\d+(?:\.\d{1,6})?$/.test(maximumText) || Number(maximumText) > 1000)
          throw new Error('最高倍率应在 0 到 1000 之间，最多六位小数')
        const auto_build = {
          ...initial,
          enabled: autoEnabled,
          models,
          size: boundedInteger(text(fields, 'build-size'), 1, 10),
          explore: boundedInteger(text(fields, 'build-explore'), 0, 9),
          schedule,
          interval_minutes: boundedInteger(text(fields, 'build-interval'), 1, 1440),
          daily_time: text(fields, 'build-time'),
          consumer_weight: Number(text(fields, 'consumer-weight')),
          success_weight: Number(text(fields, 'success-weight')),
          cache_weight: Number(text(fields, 'cache-weight')),
        }
        validateAutoBuild(auto_build)
        if (autoEnabled && !models.length) throw new Error('请选择自动更新的模型')
        if (!memberIDs.length && !autoEnabled) throw new Error('至少选择一个渠道组')
        props.onSave(
          poolInput(props.pool, {
            name: props.automatic ? 'Auto' : text(fields, 'pool-name'),
            strategy: poolStrategy(text(fields, 'strategy')),
            max_attempts: boundedInteger(text(fields, 'attempts'), 1, 32),
            failure_cooldown_seconds: boundedInteger(text(fields, 'cooldown'), 0, 3600),
            max_multiplier: Number(maximumText),
            members: orderedMembers(memberIDs),
            auto_build,
          }),
        )
      }}
    >
      {!props.automatic && (
        <Field
          name="pool-name"
          label="路由池名称"
          required
          maxLength={64}
          defaultValue={props.pool?.name}
        />
      )}
      <label className="field" htmlFor="pool-strategy">
        <span>{t('选择策略')}</span>
        <select
          id="pool-strategy"
          name="strategy"
          defaultValue={props.pool?.strategy ?? 'priority'}
        >
          <option value="priority">{t('优先级')}</option>
          <option value="weighted">{t('权重')}</option>
          <option value="round_robin">{t('轮询')}</option>
          <option value="fill_first">{t('顺序填满')}</option>
          <option value="score">{t('评分优先')}</option>
          <option value="cost">{t('低倍率优先')}</option>
        </select>
      </label>
      <Field
        name="attempts"
        label="最大尝试次数"
        type="number"
        min={1}
        max={32}
        required
        defaultValue={props.pool?.max_attempts ?? 3}
      />
      <Field
        name="cooldown"
        label="失败冷却（秒）"
        type="number"
        min={0}
        max={3600}
        required
        defaultValue={props.pool?.failure_cooldown_seconds ?? 30}
      />
      <Field
        name="maximum"
        label="最高倍率（0 不限）"
        required
        defaultValue={String(props.pool?.max_multiplier ?? 0)}
      />
      <fieldset className="pool-members">
        <legend>{t('分组顺序')}</legend>
        <p className="field-hint">{t('优先级按下方顺序保存；添加分组不会重排已有成员。')}</p>
        <ol className="pool-member-order">
          {memberIDs.map((id, index) => (
            <li key={id}>
              <span>
                {index + 1}. {groupName(id)}
              </span>
              <div className="row-actions">
                <Button
                  type="button"
                  variant="quiet"
                  disabled={props.pending || index === 0}
                  aria-label={`${t('上移')} ${groupName(id)}`}
                  onClick={() => setMemberIDs((current) => moveMember(current, index, -1))}
                >
                  {t('上移')}
                </Button>
                <Button
                  type="button"
                  variant="quiet"
                  disabled={props.pending || index === memberIDs.length - 1}
                  aria-label={`${t('下移')} ${groupName(id)}`}
                  onClick={() => setMemberIDs((current) => moveMember(current, index, 1))}
                >
                  {t('下移')}
                </Button>
                <Button
                  type="button"
                  variant="quiet"
                  disabled={props.pending}
                  onClick={() => setMemberIDs((current) => current.filter((value) => value !== id))}
                >
                  {t('移除')}
                </Button>
              </div>
            </li>
          ))}
        </ol>
        <div className="pool-group-picker">
          {props.groups.map((group) => (
            <label key={group.group_id}>
              <input
                type="checkbox"
                checked={memberIDs.includes(group.group_id)}
                disabled={props.pending}
                onChange={(event) =>
                  setMemberIDs((current) =>
                    event.target.checked
                      ? [...current, group.group_id]
                      : current.filter((id) => id !== group.group_id),
                  )
                }
              />{' '}
              {groupName(group.group_id)} · {String(group.multiplier)}×
            </label>
          ))}
        </div>
        {!props.groups.length && (
          <p className="subtle">
            {t('暂无可选择的分组；可使用的官方分组和已授权的市场分组会显示在此。')}
          </p>
        )}
      </fieldset>
      <fieldset className="pool-auto-settings">
        <legend>{t('自动更新成员')}</legend>
        <label className="pool-auto-toggle">
          <input
            type="checkbox"
            checked={autoEnabled}
            onChange={(event) => setAutoEnabled(event.target.checked)}
            disabled={props.pending}
          />{' '}
          {t('按计划自动更新路由池')}
        </label>
        <p className="field-hint">
          {t('服务器按所选模型和评分重新选择成员；停用后保留配置与当前顺序。')}
        </p>
        <div className="pool-auto-fields">
          <Field
            name="build-size"
            label="候选分组数量"
            type="number"
            min={1}
            max={10}
            required
            defaultValue={initial.size}
          />
          <Field
            name="build-explore"
            label="探索分组数量"
            type="number"
            min={0}
            max={9}
            required
            defaultValue={initial.explore}
            hint="探索数量须小于候选分组数量。"
          />
          <label className="field" htmlFor="pool-schedule">
            <span>{t('更新计划')}</span>
            <select
              id="pool-schedule"
              value={schedule}
              onChange={(event) =>
                setSchedule(event.target.value === 'daily' ? 'daily' : 'interval')
              }
            >
              <option value="interval">{t('按间隔')}</option>
              <option value="daily">{t('每日定时')}</option>
            </select>
          </label>
          <div hidden={schedule !== 'interval'}>
            <Field
              name="build-interval"
              label="更新间隔（分钟）"
              type="number"
              min={1}
              max={1440}
              required
              defaultValue={initial.interval_minutes}
            />
          </div>
          <div hidden={schedule !== 'daily'}>
            <Field
              name="build-time"
              label="每日更新时间（UTC）"
              type="time"
              required
              defaultValue={initial.daily_time}
            />
          </div>
          <Field
            name="consumer-weight"
            label="历史实扣成本权重"
            type="number"
            min={0}
            max={100}
            step={1}
            required
            defaultValue={initial.consumer_weight}
          />
          <Field
            name="success-weight"
            label="成功率权重"
            type="number"
            min={0}
            max={100}
            step={1}
            required
            defaultValue={initial.success_weight}
          />
          <Field
            name="cache-weight"
            label="缓存命中权重"
            type="number"
            min={0}
            max={100}
            step={1}
            required
            defaultValue={initial.cache_weight}
          />
        </div>
        <div className="pool-model-picker" role="group" aria-label={t('自动更新模型')}>
          {availableModels.map((model) => (
            <label key={model}>
              <input
                type="checkbox"
                checked={models.includes(model)}
                disabled={props.pending}
                onChange={(event) =>
                  setModels((current) =>
                    event.target.checked
                      ? [...current, model]
                      : current.filter((name) => name !== model),
                  )
                }
              />{' '}
              <code>{model}</code>
            </label>
          ))}
        </div>
        <p className="field-hint">
          {t('支持任一所选模型的分组可参与自动选择；实际请求按模型路由。')}
        </p>
        {!availableModels.length && <p className="subtle">{t('暂无可选模型')}</p>}
      </fieldset>
      <Button type="button" variant="quiet" disabled={props.pending} onClick={props.onCancel}>
        {t('取消')}
      </Button>
    </MarketForm>
  )
}
