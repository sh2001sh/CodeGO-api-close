import { useState } from 'react'
import type { Plan } from '../lib/commerce'
import { toMicroCredits } from '../lib/format'
import { decimalCredits } from './commerce/amounts'
import { Button } from '../components/ui'

export function readPlanModelLimits(form: FormData): NonNullable<Plan['model_limits']> {
  const names = form.getAll('plan-model-name')
  const amounts = form.getAll('plan-model-limit')
  const limits = new Map<string, bigint>()
  for (const [index, value] of names.entries()) {
    const name = String(value).trim()
    const amount = String(amounts[index] ?? '').trim()
    if (!name && !amount) continue
    if (!name || new TextEncoder().encode(name).length > 200)
      throw new Error('模型名称不能为空且最多 200 字节')
    if (limits.has(name)) throw new Error(`模型 ${name} 重复设置了额度`)
    limits.set(name, /^0(?:\.0{1,6})?$/.test(amount) ? 0n : BigInt(toMicroCredits(amount)))
  }
  return Object.fromEntries(limits)
}

export function PlanModelLimits(props: { limits: Plan['model_limits'] }) {
  const [rows, setRows] = useState(() =>
    Object.entries(props.limits ?? {}).map(([model, amount]) => ({
      id: crypto.randomUUID(),
      model,
      amount: decimalCredits(amount),
    })),
  )
  return (
    <fieldset className="full-width section">
      <legend>按模型限制消费额度</legend>
      <p className="muted">
        每个模型的消费上限以 credits 填写，最多六位小数；不设置或填写 0 表示不限制。
      </p>
      {rows.map((row) => (
        <div className="form-panel" key={row.id}>
          <label className="field" htmlFor={`model-${row.id}`}>
            <span>模型名称</span>
            <input
              id={`model-${row.id}`}
              name="plan-model-name"
              value={row.model}
              onChange={(event) =>
                setRows(
                  rows.map((item) =>
                    item.id === row.id ? { ...item, model: event.target.value } : item,
                  ),
                )
              }
            />
          </label>
          <label className="field" htmlFor={`limit-${row.id}`}>
            <span>模型消费上限 credits</span>
            <input
              id={`limit-${row.id}`}
              name="plan-model-limit"
              value={row.amount}
              onChange={(event) =>
                setRows(
                  rows.map((item) =>
                    item.id === row.id ? { ...item, amount: event.target.value } : item,
                  ),
                )
              }
            />
          </label>
          <Button
            variant="quiet"
            onClick={() => setRows(rows.filter((item) => item.id !== row.id))}
          >
            移除模型限制
          </Button>
        </div>
      ))}
      <Button
        variant="quiet"
        onClick={() => setRows([...rows, { id: crypto.randomUUID(), model: '', amount: '' }])}
      >
        添加模型限制
      </Button>
    </fieldset>
  )
}
