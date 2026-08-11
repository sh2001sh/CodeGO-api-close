import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { useForm, type Resolver } from 'react-hook-form'
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
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import type { BlindBoxTierSetting } from '../types'

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
  unitPrice: z.coerce.number().min(0),
  expireDays: z.coerce.number().int().min(1).max(365),
  registrationRewardEnabled: z.boolean(),
  registrationRewardStartAt: z.string(),
  registrationRewardEndAt: z.string(),
  dailyLimit: z.coerce.number().int().min(1),
  monthlyLimit: z.coerce.number().int().min(1),
  dailyOpenLimit: z.coerce.number().int().min(1),
  firstPurchaseGuaranteeUSD: z.coerce.number().min(0),
  pityThreshold: z.coerce.number().int().min(1),
  pityGuaranteeUSD: z.coerce.number().min(0),
  lowRewardThresholdUSD: z.coerce.number().min(0),
  subscriptionPrizeProbability: z.coerce.number().min(0).max(1),
  subscriptionPlanTitle: z.string().min(1),
  countOptions: z.string().superRefine((value, ctx) => {
    try {
      const parsed = JSON.parse(value)
      if (
        !Array.isArray(parsed) ||
        parsed.some((item) => !Number.isInteger(item) || item <= 0)
      ) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: '请输入正整数 JSON 数组',
        })
      }
    } catch {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: '请输入合法的 JSON',
      })
    }
  }),
  tiers: z.string().superRefine((value, ctx) => {
    try {
      const parsed = JSON.parse(value)
      if (!Array.isArray(parsed)) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: '请输入 JSON 数组',
        })
        return
      }
      const result = z.array(tierSchema).safeParse(parsed)
      if (!result.success) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: '每个档位都必须包含 name、min_usd、max_usd、probability',
        })
      }
    } catch {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: '请输入合法的 JSON',
      })
    }
  }),
})

type Values = z.infer<typeof schema>

function normalizeCountOptions(value: string): string {
  const parsed = JSON.parse(value) as number[]
  const unique = Array.from(
    new Set(
      parsed
        .map((item) => Number(item))
        .filter((item) => Number.isInteger(item) && item > 0)
    )
  ).sort((left, right) => left - right)
  return JSON.stringify(unique)
}

function normalizeTiers(value: string): string {
  const parsed = z.array(tierSchema).parse(JSON.parse(value))
  return JSON.stringify(parsed)
}

function stringifyJson(value: unknown): string {
  return JSON.stringify(value, null, 2)
}

function formatDateTimeLocal(timestamp: number): string {
  if (!timestamp) return ''
  const date = new Date(timestamp * 1000)
  date.setMinutes(date.getMinutes() - date.getTimezoneOffset())
  return date.toISOString().slice(0, 16)
}

function parseDateTimeLocal(value: string): number {
  const timestamp = new Date(value).getTime()
  return Number.isFinite(timestamp) ? Math.floor(timestamp / 1000) : 0
}

export function BlindBoxSettingsSection({
  defaultValues,
}: {
  defaultValues: {
    enabled: boolean
    unitPrice: number
    expireDays: number
    registrationRewardEnabled: boolean
    registrationRewardStartAt: number
    registrationRewardEndAt: number
    dailyLimit: number
    monthlyLimit: number
    dailyOpenLimit: number
    firstPurchaseGuaranteeUSD: number
    pityThreshold: number
    pityGuaranteeUSD: number
    lowRewardThresholdUSD: number
    subscriptionPrizeProbability: number
    subscriptionPlanTitle: string
    countOptions: number[]
    tiers: BlindBoxTierSetting[]
  }
}) {
  const updateOption = useUpdateOption()

  const form = useForm<Values>({
    resolver: zodResolver(schema) as unknown as Resolver<Values>,
    defaultValues: {
      enabled: defaultValues.enabled,
      unitPrice: defaultValues.unitPrice,
      expireDays: defaultValues.expireDays,
      registrationRewardEnabled: defaultValues.registrationRewardEnabled,
      registrationRewardStartAt: formatDateTimeLocal(
        defaultValues.registrationRewardStartAt
      ),
      registrationRewardEndAt: formatDateTimeLocal(
        defaultValues.registrationRewardEndAt
      ),
      dailyLimit: defaultValues.dailyLimit,
      monthlyLimit: defaultValues.monthlyLimit,
      dailyOpenLimit: defaultValues.dailyOpenLimit,
      firstPurchaseGuaranteeUSD: defaultValues.firstPurchaseGuaranteeUSD,
      pityThreshold: defaultValues.pityThreshold,
      pityGuaranteeUSD: defaultValues.pityGuaranteeUSD,
      lowRewardThresholdUSD: defaultValues.lowRewardThresholdUSD,
      subscriptionPrizeProbability: defaultValues.subscriptionPrizeProbability,
      subscriptionPlanTitle: defaultValues.subscriptionPlanTitle,
      countOptions: stringifyJson(defaultValues.countOptions),
      tiers: stringifyJson(defaultValues.tiers),
    },
  })

  const { isDirty, isSubmitting } = form.formState
  const enabled = form.watch('enabled')
  const registrationRewardEnabled = form.watch('registrationRewardEnabled')

  async function onSubmit(values: Values) {
    const normalizedCountOptions = normalizeCountOptions(values.countOptions)
    const normalizedTiers = normalizeTiers(values.tiers)
    const defaultCountOptions = JSON.stringify(defaultValues.countOptions)
    const defaultTiers = JSON.stringify(defaultValues.tiers)
    const registrationRewardStartAt = parseDateTimeLocal(
      values.registrationRewardStartAt
    )
    const registrationRewardEndAt = parseDateTimeLocal(
      values.registrationRewardEndAt
    )

    if (
      registrationRewardStartAt > 0 &&
      registrationRewardEndAt > 0 &&
      registrationRewardEndAt <= registrationRewardStartAt
    ) {
      form.setError('registrationRewardEndAt', {
        message: '结束时间必须晚于开始时间',
      })
      return
    }

    const updates: Array<{ key: string; value: string }> = []

    const pushIfChanged = (
      key: string,
      next: string | number | boolean,
      previous: string | number | boolean
    ) => {
      if (next !== previous) {
        updates.push({ key, value: String(next) })
      }
    }

    pushIfChanged('blind_box_setting.enabled', values.enabled, defaultValues.enabled)
    pushIfChanged(
      'blind_box_setting.unit_price',
      values.unitPrice,
      defaultValues.unitPrice
    )
    pushIfChanged(
      'blind_box_setting.expire_days',
      values.expireDays,
      defaultValues.expireDays
    )
    pushIfChanged(
      'blind_box_setting.registration_reward_enabled',
      values.registrationRewardEnabled,
      defaultValues.registrationRewardEnabled
    )
    pushIfChanged(
      'blind_box_setting.registration_reward_start_at',
      registrationRewardStartAt,
      defaultValues.registrationRewardStartAt
    )
    pushIfChanged(
      'blind_box_setting.registration_reward_end_at',
      registrationRewardEndAt,
      defaultValues.registrationRewardEndAt
    )
    pushIfChanged(
      'blind_box_setting.daily_limit',
      values.dailyLimit,
      defaultValues.dailyLimit
    )
    pushIfChanged(
      'blind_box_setting.monthly_limit',
      values.monthlyLimit,
      defaultValues.monthlyLimit
    )
    pushIfChanged(
      'blind_box_setting.daily_open_limit',
      values.dailyOpenLimit,
      defaultValues.dailyOpenLimit
    )
    pushIfChanged(
      'blind_box_setting.first_purchase_guarantee_usd',
      values.firstPurchaseGuaranteeUSD,
      defaultValues.firstPurchaseGuaranteeUSD
    )
    pushIfChanged(
      'blind_box_setting.pity_threshold',
      values.pityThreshold,
      defaultValues.pityThreshold
    )
    pushIfChanged(
      'blind_box_setting.pity_guarantee_usd',
      values.pityGuaranteeUSD,
      defaultValues.pityGuaranteeUSD
    )
    pushIfChanged(
      'blind_box_setting.low_reward_threshold_usd',
      values.lowRewardThresholdUSD,
      defaultValues.lowRewardThresholdUSD
    )
    pushIfChanged(
      'blind_box_setting.subscription_prize_probability',
      values.subscriptionPrizeProbability,
      defaultValues.subscriptionPrizeProbability
    )
    pushIfChanged(
      'blind_box_setting.subscription_plan_title',
      values.subscriptionPlanTitle.trim(),
      defaultValues.subscriptionPlanTitle
    )

    if (normalizedCountOptions !== defaultCountOptions) {
      updates.push({
        key: 'blind_box_setting.count_options',
        value: normalizedCountOptions,
      })
    }
    if (normalizedTiers !== defaultTiers) {
      updates.push({
        key: 'blind_box_setting.tiers',
        value: normalizedTiers,
      })
    }

    if (updates.length === 0) {
      toast.info('没有需要保存的变更')
      return
    }

    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }

    form.reset({
      ...values,
      subscriptionPlanTitle: values.subscriptionPlanTitle.trim(),
      countOptions: stringifyJson(JSON.parse(normalizedCountOptions)),
      tiers: stringifyJson(JSON.parse(normalizedTiers)),
    })
  }

  return (
    <SettingsSection
      title='盲盒活动'
      description='配置盲盒售价、首购专属奖池、常规奖池概率和保底机制'
    >
      <Form {...form}>
        <form onSubmit={form.handleSubmit(onSubmit)} className='space-y-6'>
          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                <div className='space-y-0.5'>
                  <FormLabel className='text-base'>启用盲盒活动</FormLabel>
                  <FormDescription>
                    在钱包页展示盲盒购买、支付和开奖入口
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={updateOption.isPending || isSubmitting}
                  />
                </FormControl>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='registrationRewardEnabled'
            render={({ field }) => (
              <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                <div className='space-y-0.5'>
                  <FormLabel className='text-base'>启用邀请码注册赠盒</FormLabel>
                  <FormDescription>
                    仅在活动有效期内，填写有效邀请码的新用户才获得 5 个未开启盲盒
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={updateOption.isPending || isSubmitting}
                  />
                </FormControl>
              </FormItem>
            )}
          />

          <div className='grid gap-6 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='registrationRewardStartAt'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>邀请赠盒活动开始时间</FormLabel>
                  <FormControl>
                    <Input
                      type='datetime-local'
                      disabled={!registrationRewardEnabled}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>留空表示保存后立即生效</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='registrationRewardEndAt'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>邀请赠盒活动结束时间</FormLabel>
                  <FormControl>
                    <Input
                      type='datetime-local'
                      disabled={!registrationRewardEnabled}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>留空表示不设截止时间</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div className='grid gap-6 md:grid-cols-2 xl:grid-cols-4'>
            <FormField
              control={form.control}
              name='unitPrice'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>单盒售价（USD）</FormLabel>
                  <FormControl>
                    <Input type='number' step='0.01' min={0} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='dailyLimit'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>单用户每日购买上限</FormLabel>
                  <FormControl>
                    <Input type='number' min={1} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='expireDays'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>盲盒有效期（天）</FormLabel>
                  <FormControl>
                    <Input type='number' min={1} max={365} {...field} />
                  </FormControl>
                  <FormDescription>
                    邀请码注册赠送和购买获得的未开启盲盒，到期后不可再开启
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='monthlyLimit'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>单用户每月购买上限</FormLabel>
                  <FormControl>
                    <Input type='number' min={1} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div className='grid gap-6 md:grid-cols-2 xl:grid-cols-4'>
            <FormField
              control={form.control}
              name='dailyOpenLimit'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>全站每日开奖上限</FormLabel>
                  <FormControl>
                    <Input type='number' min={1} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='firstPurchaseGuaranteeUSD'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>首购奖池起始金额（USD）</FormLabel>
                  <FormControl>
                    <Input type='number' step='0.01' min={0} {...field} />
                  </FormControl>
                  <FormDescription>
                    首购首盒若未命中月卡大奖，就进入专属首购奖池。该奖池保持月卡大奖概率不变，非月卡奖励区间从这里开始，并提升高档位概率。
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='pityThreshold'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>保底触发次数</FormLabel>
                  <FormControl>
                    <Input type='number' min={1} {...field} />
                  </FormControl>
                  <FormDescription>
                    连续抽到低档奖励达到该次数后，下一次触发保底
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='pityGuaranteeUSD'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>保底最低奖励（USD）</FormLabel>
                  <FormControl>
                    <Input type='number' step='0.01' min={0} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='lowRewardThresholdUSD'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>低档奖励判定线（USD）</FormLabel>
                  <FormControl>
                    <Input type='number' step='0.01' min={0} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div className='grid gap-6 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='subscriptionPrizeProbability'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>月卡大奖概率</FormLabel>
                  <FormControl>
                    <Input type='number' step='0.0001' min={0} max={1} {...field} />
                  </FormControl>
                  <FormDescription>
                    使用小数概率，例如 0.003 代表 0.3%
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='subscriptionPlanTitle'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>月卡奖励标题</FormLabel>
                  <FormControl>
                    <Input {...field} />
                  </FormControl>
                  <FormDescription>
                    用户抽中稀有月卡大奖时展示该标题
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          {enabled ? (
            <>
              <FormField
                control={form.control}
                name='countOptions'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>购买数量选项</FormLabel>
                    <FormControl>
                      <Textarea rows={4} {...field} />
                    </FormControl>
                    <FormDescription>
                      JSON 数组，例如 [1, 5, 10, 20, 50]
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='tiers'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>常规奖励档位</FormLabel>
                    <FormControl>
                      <Textarea rows={10} {...field} />
                    </FormControl>
                    <FormDescription>
                      JSON 数组，支持 name、min_usd、max_usd、probability、reward_type、wallet_type
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </>
          ) : null}

          <Button
            type='submit'
            disabled={!isDirty || updateOption.isPending || isSubmitting}
          >
            {updateOption.isPending || isSubmitting ? '保存中...' : '保存盲盒配置'}
          </Button>
        </form>
      </Form>
    </SettingsSection>
  )
}
