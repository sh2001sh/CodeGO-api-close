import { useEffect, useMemo } from 'react'
import { z } from 'zod'
import { useForm, type Resolver } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { Settings2 } from 'lucide-react'
import { toast } from 'sonner'
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
import type { BlindBoxTierSetting } from '@/features/system-settings/types'
import { BALANCE_BLIND_BOX_DEFAULTS } from './balance-blind-box-settings-data'

const tierSchema = z.object({
  name: z.string().min(1),
  min_usd: z.number().min(0),
  max_usd: z.number().min(0),
  probability: z.number().min(0).max(1),
  reward_type: z.string().optional(),
  wallet_type: z.string().optional(),
})

const schema = z.object({
  enabled: z.boolean(),
  priceUSD: z.coerce.number().positive().max(10000),
  dailyPurchaseLimit: z.coerce.number().int().min(1).max(10000),
  firstDrawGuaranteeUSD: z.coerce.number().min(0).max(100000),
  smallPityThreshold: z.coerce.number().int().min(1).max(10000),
  smallPityGuaranteeUSD: z.coerce.number().min(0).max(100000),
  pityThreshold: z.coerce.number().int().min(1).max(10000),
  pityGuaranteeUSD: z.coerce.number().min(0).max(100000),
  tiers: z.string().superRefine((value, context) => {
    const parsed = parseTiers(value)
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
  const probability = calculateProbability(tiersValue)

  useEffect(() => {
    if (!form.formState.isDirty) form.reset(defaults)
  }, [defaults, form])

  const onSubmit = async (values: Values) => {
    const normalizedTiers = JSON.stringify(parseTiers(values.tiers))
    const updates = buildUpdates(values, settings, normalizedTiers)
    if (updates.length === 0) {
      toast.info('没有需要保存的统一盲盒变更')
      return
    }
    for (const update of updates) await updateOption.mutateAsync(update)
    form.reset({ ...values, tiers: formatTiers(normalizedTiers) })
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
            控制统一盲盒售价、单用户每日购买数量与统一奖池。人民币和统一额度入口使用同一库存与概率表。
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

          <FormField
            control={form.control}
            name='tiers'
            render={({ field }) => (
              <FormItem>
                <div className='flex flex-wrap items-center justify-between gap-2'>
                  <FormLabel>统一盲盒奖池</FormLabel>
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
                  wallet_type，所有 probability 合计必须等于 1。
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
    tiers: JSON.stringify(
      settings['blind_box_setting.balance_blind_box_tiers'],
      null,
      2
    ),
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

function parseTiers(value: string): BlindBoxTierSetting[] | null {
  try {
    const parsed = z.array(tierSchema).safeParse(JSON.parse(value))
    return parsed.success ? parsed.data : null
  } catch {
    return null
  }
}

function calculateProbability(value: string) {
  const tiers = parseTiers(value)
  return tiers?.reduce((sum, tier) => sum + tier.probability, 0) ?? null
}

function formatTiers(value: string) {
  return JSON.stringify(JSON.parse(value), null, 2)
}
