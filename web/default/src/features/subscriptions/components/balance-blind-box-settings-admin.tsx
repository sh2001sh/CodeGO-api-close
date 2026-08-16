import { useEffect, useMemo } from 'react'
import { z } from 'zod'
import { useForm, type Resolver } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { Gauge, Settings2, TrendingUp } from 'lucide-react'
import { toast } from 'sonner'
import { calculateBlindBoxEconomics } from '@/lib/blind-box-economics'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import {
  getOptionValue,
  useSystemOptions,
} from '@/features/system-settings/hooks/use-system-options'
import { useUpdateOption } from '@/features/system-settings/hooks/use-update-option'
import { BALANCE_BLIND_BOX_DEFAULTS } from './balance-blind-box-settings-data'
import {
  calculateTierProbability,
  formatBlindBoxTiers,
  normalizeDisplayedTiers,
  parseBlindBoxTiers,
} from './balance-blind-box-settings-utils'

const schema = z.object({
  enabled: z.boolean(),
  priceUSD: z.coerce.number().positive().max(10000),
  dailyPurchaseLimit: z.coerce.number().int().min(1).max(10),
  firstDrawGuaranteeUSD: z.coerce.number().min(0).max(100000),
  smallPityThreshold: z.coerce.number().int().min(1).max(10000),
  smallPityGuaranteeUSD: z.coerce.number().min(0).max(100000),
  pityThreshold: z.coerce.number().int().min(1).max(10000),
  pityGuaranteeUSD: z.coerce.number().min(0).max(100000),
  tiers: z.string().superRefine((value, context) => {
    const parsed = parseBlindBoxTiers(value)
    if (!parsed) {
      context.addIssue({
        code: z.ZodIssueCode.custom,
        message: '请输入合法的奖池 JSON 数组',
      })
      return
    }
    if (parsed.some((tier) => tier.max_usd < tier.min_usd)) {
      context.addIssue({
        code: z.ZodIssueCode.custom,
        message: '奖励区间最大值不能小于最小值',
      })
      return
    }
    const probability = parsed.reduce((sum, tier) => sum + tier.probability, 0)
    if (Math.abs(probability - 1) > 0.000001) {
      context.addIssue({
        code: z.ZodIssueCode.custom,
        message: `奖池概率合计必须为 100%，当前为 ${(probability * 100).toFixed(4)}%`,
      })
    }
  }),
})

type Values = z.infer<typeof schema>

export function BalanceBlindBoxSettingsAdmin() {
  const optionsQuery = useSystemOptions()
  const updateOption = useUpdateOption()
  const settings = getOptionValue(
    optionsQuery.data?.data,
    BALANCE_BLIND_BOX_DEFAULTS
  )
  const defaults = useMemo(() => toFormValues(settings), [settings])
  const form = useForm<Values>({
    resolver: zodResolver(schema) as unknown as Resolver<Values>,
    defaultValues: defaults,
  })
  const tiersValue = form.watch('tiers')
  const parsedTiers = parseBlindBoxTiers(tiersValue)
  const probability = calculateTierProbability(tiersValue)
  const economics = parsedTiers
    ? calculateBlindBoxEconomics(parsedTiers, form.watch('priceUSD'))
    : null

  useEffect(() => {
    if (!form.formState.isDirty) form.reset(defaults)
  }, [defaults, form])

  const onSubmit = async (values: Values) => {
    const normalizedTiers = JSON.stringify(parseBlindBoxTiers(values.tiers))
    const updates = buildUpdates(values, settings, normalizedTiers)
    if (updates.length === 0) {
      toast.info('没有需要保存的统一盲盒变更')
      return
    }
    for (const update of updates) await updateOption.mutateAsync(update)
    form.reset({ ...values, tiers: formatBlindBoxTiers(normalizedTiers) })
    await optionsQuery.refetch()
  }

  return (
    <section className='rounded-2xl border p-4'>
      <div className='flex items-start gap-3'>
        <div className='bg-primary/10 text-primary flex size-9 shrink-0 items-center justify-center rounded-lg'>
          <Settings2 className='size-4' />
        </div>
        <div>
          <h3 className='text-sm font-semibold'>统一盲盒管理</h3>
          <p className='text-muted-foreground mt-1 text-sm leading-6'>
            控制统一盲盒售价、单用户每日购买数量与普通高方差奖池。人民币和统一额度入口使用同一库存与概率表，首购和大小保底使用独立有界奖池。
          </p>
        </div>
      </div>

      <Form {...form}>
        <form onSubmit={form.handleSubmit(onSubmit)} className='mt-5 space-y-5'>
          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <FormItem className='bg-muted/40 flex items-center justify-between rounded-xl p-3'>
                <div>
                  <FormLabel>启用统一盲盒</FormLabel>
                  <FormDescription>
                    关闭后用户端不再允许购买或开启
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={updateOption.isPending}
                  />
                </FormControl>
              </FormItem>
            )}
          />

          <div className='grid gap-4 sm:grid-cols-2 xl:grid-cols-4'>
            <NumberField
              form={form}
              name='priceUSD'
              label='单盒售价（USD）'
              step='0.01'
            />
            <NumberField
              form={form}
              name='dailyPurchaseLimit'
              label='单用户每日购买上限'
            />
            <NumberField
              form={form}
              name='firstDrawGuaranteeUSD'
              label='首抽最低等值额度（USD）'
              step='0.01'
            />
            <NumberField
              form={form}
              name='smallPityThreshold'
              label='小保底触发抽数'
            />
            <NumberField
              form={form}
              name='smallPityGuaranteeUSD'
              label='小保底最低等值额度（USD）'
              step='0.01'
            />
            <NumberField
              form={form}
              name='pityThreshold'
              label='大保底触发抽数'
            />
            <NumberField
              form={form}
              name='pityGuaranteeUSD'
              label='大保底最低等值额度（USD）'
              step='0.01'
            />
          </div>

          <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'>
            <EconomicsMetric
              icon={TrendingUp}
              label='普通池理论期望'
              value={
                economics
                  ? `${economics.expectedRewardUSD.toFixed(3)} USD`
                  : '--'
              }
            />
            <EconomicsMetric
              icon={Gauge}
              label='普通池理论回报率'
              value={economics ? `${economics.returnRate.toFixed(2)}%` : '--'}
            />
            <EconomicsMetric
              icon={TrendingUp}
              label='单抽额度不低于售价概率'
              value={
                economics
                  ? `${(economics.immediateProfitProbability * 100).toFixed(2)}%`
                  : '--'
              }
            />
            <EconomicsMetric
              icon={Gauge}
              label='普通池单奖上限'
              value={
                economics ? `${economics.maxRewardUSD.toFixed(2)} USD` : '--'
              }
            />
          </div>
          <p className='text-muted-foreground text-xs leading-5'>
            理论指标包含“再来一抽”的连锁期望，不包含首抽、小保底和大保底；用户短期实际回报会围绕理论值波动。
          </p>

          <FormField
            control={form.control}
            name='tiers'
            render={({ field }) => (
              <FormItem>
                <div className='flex flex-wrap items-center justify-between gap-2'>
                  <FormLabel>普通高方差奖池</FormLabel>
                  <span
                    className={
                      probability !== null &&
                      Math.abs(probability - 1) <= 0.000001
                        ? 'text-emerald-700 dark:text-emerald-300'
                        : 'text-destructive'
                    }
                  >
                    概率合计：
                    {probability === null
                      ? 'JSON 无效'
                      : `${(probability * 100).toFixed(4)}%`}
                  </span>
                </div>
                <FormControl>
                  <Textarea
                    rows={12}
                    className='font-mono text-xs'
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  支持 name、min_usd、max_usd、probability、reward_type 和
                  wallet_type，所有 probability 合计必须等于
                  1。这里不包含首购、小保底和大保底池。
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <Button
            type='submit'
            disabled={!form.formState.isDirty || updateOption.isPending}
          >
            {updateOption.isPending ? '保存中...' : '保存统一盲盒配置'}
          </Button>
        </form>
      </Form>
    </section>
  )
}

function EconomicsMetric(props: {
  icon: typeof Gauge
  label: string
  value: string
}) {
  const Icon = props.icon
  return (
    <div className='bg-muted/35 rounded-lg border px-3 py-2.5'>
      <div className='text-muted-foreground flex items-center gap-1.5 text-xs'>
        <Icon className='size-3.5' aria-hidden='true' />
        {props.label}
      </div>
      <div className='mt-1 text-sm font-semibold tabular-nums'>
        {props.value}
      </div>
    </div>
  )
}

function NumberField(props: {
  form: ReturnType<typeof useForm<Values>>
  name: Exclude<keyof Values, 'enabled' | 'tiers'>
  label: string
  step?: string
}) {
  return (
    <FormField
      control={props.form.control}
      name={props.name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{props.label}</FormLabel>
          <FormControl>
            <Input type='number' min={0} step={props.step || '1'} {...field} />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

function toFormValues(settings: typeof BALANCE_BLIND_BOX_DEFAULTS): Values {
  const tiers = normalizeDisplayedTiers(
    settings['blind_box_setting.balance_blind_box_tiers']
  )
  return {
    enabled: settings['blind_box_setting.balance_blind_box_enabled'],
    priceUSD: settings['blind_box_setting.balance_blind_box_price_usd'],
    dailyPurchaseLimit:
      settings['blind_box_setting.balance_blind_box_daily_purchase_limit'],
    firstDrawGuaranteeUSD:
      settings['blind_box_setting.balance_blind_box_first_draw_guarantee_usd'],
    smallPityThreshold:
      settings['blind_box_setting.balance_blind_box_small_pity_threshold'],
    smallPityGuaranteeUSD:
      settings['blind_box_setting.balance_blind_box_small_pity_guarantee_usd'],
    pityThreshold:
      settings['blind_box_setting.balance_blind_box_pity_threshold'],
    pityGuaranteeUSD:
      settings['blind_box_setting.balance_blind_box_pity_guarantee_usd'],
    tiers: JSON.stringify(tiers, null, 2),
  }
}

function buildUpdates(
  values: Values,
  settings: typeof BALANCE_BLIND_BOX_DEFAULTS,
  tiers: string
) {
  const pairs: Array<
    [string, string | number | boolean, string | number | boolean]
  > = [
    [
      'blind_box_setting.balance_blind_box_enabled',
      values.enabled,
      settings['blind_box_setting.balance_blind_box_enabled'],
    ],
    [
      'blind_box_setting.balance_blind_box_price_usd',
      values.priceUSD,
      settings['blind_box_setting.balance_blind_box_price_usd'],
    ],
    [
      'blind_box_setting.balance_blind_box_daily_purchase_limit',
      values.dailyPurchaseLimit,
      settings['blind_box_setting.balance_blind_box_daily_purchase_limit'],
    ],
    [
      'blind_box_setting.balance_blind_box_first_draw_guarantee_usd',
      values.firstDrawGuaranteeUSD,
      settings['blind_box_setting.balance_blind_box_first_draw_guarantee_usd'],
    ],
    [
      'blind_box_setting.balance_blind_box_small_pity_threshold',
      values.smallPityThreshold,
      settings['blind_box_setting.balance_blind_box_small_pity_threshold'],
    ],
    [
      'blind_box_setting.balance_blind_box_small_pity_guarantee_usd',
      values.smallPityGuaranteeUSD,
      settings['blind_box_setting.balance_blind_box_small_pity_guarantee_usd'],
    ],
    [
      'blind_box_setting.balance_blind_box_pity_threshold',
      values.pityThreshold,
      settings['blind_box_setting.balance_blind_box_pity_threshold'],
    ],
    [
      'blind_box_setting.balance_blind_box_pity_guarantee_usd',
      values.pityGuaranteeUSD,
      settings['blind_box_setting.balance_blind_box_pity_guarantee_usd'],
    ],
    [
      'blind_box_setting.balance_blind_box_tiers',
      tiers,
      JSON.stringify(settings['blind_box_setting.balance_blind_box_tiers']),
    ],
  ]
  return pairs
    .filter(([, next, previous]) => next !== previous)
    .map(([key, value]) => ({ key, value: String(value) }))
}
