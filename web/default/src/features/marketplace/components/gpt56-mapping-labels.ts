import type { GPT56MappingLevel, GPT56MappingTrigger } from '../types'

export function mappingLevelLabel(
  level: GPT56MappingLevel,
  t: (value: string) => string
) {
  return level === 'daily_light'
    ? t('轻量检测 · 每模型 9 次')
    : t('确认检测 · 每模型 30 次')
}

export function mappingTriggerLabel(
  trigger: GPT56MappingTrigger,
  t: (value: string) => string
) {
  const labels: Record<GPT56MappingTrigger, string> = {
    scheduled: '每日定时',
    manual: '人工触发',
    initial: '首次发布',
    confirmation: '异常自动复检',
  }
  return t(labels[trigger])
}
