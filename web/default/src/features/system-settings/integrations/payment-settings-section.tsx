/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import * as React from 'react'
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { getIntlLocale } from '@/i18n/format'
import { Code2, Eye, ShieldAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from '@/components/ui/alert'
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
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { RiskAcknowledgementDialog } from '@/components/risk-acknowledgement-dialog'
import { confirmPaymentCompliance } from '../api'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import { handleSettingsAction } from '../utils/settings-action'
import { AmountDiscountVisualEditor } from './amount-discount-visual-editor'
import { AmountOptionsVisualEditor } from './amount-options-visual-editor'
import { CreemProductsVisualEditor } from './creem-products-visual-editor'
import { PaymentMethodsVisualEditor } from './payment-methods-visual-editor'
import {
  formatJsonForEditor,
  getJsonError,
  normalizeJsonForComparison,
  removeTrailingSlash,
} from './utils'
import {
  WaffoPancakeSettingsSection,
  type WaffoPancakeSettingsValues,
} from './waffo-pancake-settings-section'
import {
  WaffoSettingsSection,
  type WaffoSettingsValues,
} from './waffo-settings-section'

const paymentSchema = z.object({
  PayAddress: z.string().refine((value) => {
    const trimmed = value.trim()
    if (!trimmed) return true
    return /^https?:\/\//.test(trimmed)
  }, 'Provide a valid callback URL starting with http:// or https://'),
  EpayId: z.string(),
  EpayKey: z.string(),
  Price: z.coerce.number().min(0),
  MinTopUp: z.coerce.number().min(0),
  CustomCallbackAddress: z.string().refine((value) => {
    const trimmed = value.trim()
    if (!trimmed) return true
    return /^https?:\/\//.test(trimmed)
  }, 'Provide a valid URL starting with http:// or https://'),
  PayMethods: z.string().superRefine((value, ctx) => {
    const error = getJsonError(value)
    if (error) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: error,
      })
    }
  }),
  AmountOptions: z.string().superRefine((value, ctx) => {
    const error = getJsonError(value, (parsed) => Array.isArray(parsed))
    if (error) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: error,
      })
    }
  }),
  AmountDiscount: z.string().superRefine((value, ctx) => {
    const error = getJsonError(
      value,
      (parsed) =>
        !!parsed && typeof parsed === 'object' && !Array.isArray(parsed)
    )
    if (error) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: error,
      })
    }
  }),
  StripeApiSecret: z.string(),
  StripeWebhookSecret: z.string(),
  StripePriceId: z.string(),
  StripeCNYUnitPrice: z.coerce.number().positive().finite(),
  StripeHKDUnitPrice: z.coerce.number().positive().finite(),
  StripeUSDUnitPrice: z.coerce.number().positive().finite(),
  StripeUnitPrice: z.coerce.number().min(0),
  StripeMinTopUp: z.coerce.number().int().min(5),
  StripePromotionCodesEnabled: z.boolean(),
  CreemApiKey: z.string(),
  CreemWebhookSecret: z.string(),
  CreemTestMode: z.boolean(),
  CreemProducts: z.string().superRefine((value, ctx) => {
    const error = getJsonError(value, (parsed) => Array.isArray(parsed))
    if (error) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: error,
      })
    }
  }),
  XunhuEnabled: z.boolean(),
  XunhuAppID: z.string(),
  XunhuSecret: z.string(),
  XunhuGateway: z.string(),
  XunhuMinTopUp: z.coerce.number().min(0),
  NowPaymentsEnabled: z.boolean(),
  NowPaymentsApiKey: z.string(),
  NowPaymentsIPNSecret: z.string(),
  NowPaymentsPaymentCurrency: z
    .string()
    .refine(
      (value) => value.trim().toLowerCase() === 'usdt',
      'NOWPayments only supports USDT'
    ),
  NowPaymentsPayCurrency: z.string().min(1),
  NowPaymentsQuotaPerUSDT: z.coerce.number().positive(),
  NowPaymentsMinTopUp: z.coerce.number().int().min(1),
})

type PaymentFormValues = z.infer<typeof paymentSchema>

const CURRENT_COMPLIANCE_TERMS_VERSION = 'v1'

type PaymentComplianceDefaults = {
  confirmed: boolean
  termsVersion: string
  confirmedAt: number
  confirmedBy: number
}

type PaymentSettingsSectionProps = {
  defaultValues: PaymentFormValues
  waffoDefaultValues: WaffoSettingsValues
  waffoPancakeDefaultValues: WaffoPancakeSettingsValues
  complianceDefaults: PaymentComplianceDefaults
}

export function PaymentSettingsSection({
  defaultValues,
  waffoDefaultValues,
  waffoPancakeDefaultValues,
  complianceDefaults,
}: PaymentSettingsSectionProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const updateOption = useUpdateOption()
  const initialRef = React.useRef(defaultValues)
  const defaultsSignature = React.useMemo(
    () => JSON.stringify(defaultValues),
    [defaultValues]
  )

  const [payMethodsVisualMode, setPayMethodsVisualMode] = React.useState(true)
  const [amountOptionsVisualMode, setAmountOptionsVisualMode] =
    React.useState(true)
  const [amountDiscountVisualMode, setAmountDiscountVisualMode] =
    React.useState(true)
  const [creemProductsVisualMode, setCreemProductsVisualMode] =
    React.useState(true)
  const [showComplianceDialog, setShowComplianceDialog] = React.useState(false)

  const complianceStatements = React.useMemo(
    () => [
      t(
        'You have legally obtained authorization for the connected model APIs, accounts, keys, and quotas.'
      ),
      t(
        'You commit to using upstream APIs, accounts, keys, quotas, and service capabilities only within the scope of lawful authorization obtained from upstream service providers, model service providers, or relevant rights holders, and will not conduct unauthorized resale, trafficking, distribution, or other non-compliant commercialization.'
      ),
      t(
        'If you provide generative AI services to the public in mainland China, you will fulfill legal obligations including filing, security assessment, content safety, complaint handling, generated content labeling, log retention, and personal information protection.'
      ),
      t(
        'You commit not to use this system to implement, assist with, or indirectly implement acts that violate applicable laws and regulations, regulatory requirements, platform rules, public interests, or the lawful rights and interests of third parties.'
      ),
      t(
        'You understand and independently bear legal responsibility arising from deployment, operation, and charging behavior.'
      ),
      t(
        'You understand this compliance reminder is only for risk notice and does not constitute legal advice, a compliance review conclusion, or a guarantee of the legality of your use of this system; you should consult professional legal or compliance advisors based on your actual business scenario.'
      ),
    ],
    [t]
  )

  const complianceRequiredText = t(
    'I have read and understood the above compliance reminder, acknowledge the related legal risks, and confirm that I bear legal responsibility arising from deployment, operation, and charging behavior.'
  )
  const complianceRequiredTextParts = React.useMemo(
    () => [
      {
        type: 'input' as const,
        text: t('I have read and understood the above compliance reminder'),
      },
      { type: 'static' as const, text: t('，') },
      {
        type: 'input' as const,
        text: t('acknowledge the related legal risks'),
      },
      { type: 'static' as const, text: t('，and ') },
      {
        type: 'input' as const,
        text: t(
          'confirm that I bear legal responsibility arising from deployment'
        ),
      },
      { type: 'static' as const, text: t('、') },
      {
        type: 'input' as const,
        text: t('operation and charging behavior'),
      },
    ],
    [t]
  )

  const complianceConfirmed =
    complianceDefaults.confirmed &&
    complianceDefaults.termsVersion === CURRENT_COMPLIANCE_TERMS_VERSION

  const confirmComplianceMutation = useMutation({
    mutationFn: confirmPaymentCompliance,
    onSuccess: (data) => {
      if (data.success) {
        toast.success(t('Compliance confirmed successfully'))
        setShowComplianceDialog(false)
        queryClient.invalidateQueries({ queryKey: ['system-options'] })
      } else {
        toast.error(data.message || t('Failed to confirm compliance'))
      }
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to confirm compliance'))
    },
  })

  const form = useForm({
    resolver: zodResolver(paymentSchema),
    mode: 'onChange', // Enable real-time validation
    defaultValues: {
      ...defaultValues,
      PayMethods: formatJsonForEditor(defaultValues.PayMethods),
      AmountOptions: formatJsonForEditor(defaultValues.AmountOptions),
      AmountDiscount: formatJsonForEditor(defaultValues.AmountDiscount),
      CreemProducts: formatJsonForEditor(defaultValues.CreemProducts),
    },
  })

  React.useEffect(() => {
    if (
      form.formState.isDirty ||
      form.formState.isSubmitting ||
      updateOption.isPending
    )
      return
    const parsedDefaults = JSON.parse(defaultsSignature) as PaymentFormValues
    initialRef.current = parsedDefaults
    form.reset({
      ...parsedDefaults,
      PayMethods: formatJsonForEditor(parsedDefaults.PayMethods),
      AmountOptions: formatJsonForEditor(parsedDefaults.AmountOptions),
      AmountDiscount: formatJsonForEditor(parsedDefaults.AmountDiscount),
      CreemProducts: formatJsonForEditor(parsedDefaults.CreemProducts),
    })
  }, [
    defaultsSignature,
    form,
    form.formState.isDirty,
    form.formState.isSubmitting,
    updateOption.isPending,
  ])

  const saveGeneralSettings = async () => {
    const values = form.getValues()
    const sanitized = {
      Price: values.Price as number,
      MinTopUp: values.MinTopUp as number,
      PayMethods: values.PayMethods.trim(),
      AmountOptions: values.AmountOptions.trim(),
      AmountDiscount: values.AmountDiscount.trim(),
    }

    const initial = {
      Price: initialRef.current.Price,
      MinTopUp: initialRef.current.MinTopUp,
      PayMethods: initialRef.current.PayMethods.trim(),
      AmountOptions: initialRef.current.AmountOptions.trim(),
      AmountDiscount: initialRef.current.AmountDiscount.trim(),
    }

    const updates: Array<{ key: string; value: string | number }> = []

    if (sanitized.Price !== initial.Price) {
      updates.push({ key: 'Price', value: sanitized.Price })
    }

    if (sanitized.MinTopUp !== initial.MinTopUp) {
      updates.push({ key: 'MinTopUp', value: sanitized.MinTopUp })
    }

    if (
      normalizeJsonForComparison(sanitized.PayMethods) !==
      normalizeJsonForComparison(initial.PayMethods)
    ) {
      updates.push({ key: 'PayMethods', value: sanitized.PayMethods })
    }

    if (
      normalizeJsonForComparison(sanitized.AmountOptions) !==
      normalizeJsonForComparison(initial.AmountOptions)
    ) {
      updates.push({
        key: 'payment_setting.amount_options',
        value: sanitized.AmountOptions,
      })
    }

    if (
      normalizeJsonForComparison(sanitized.AmountDiscount) !==
      normalizeJsonForComparison(initial.AmountDiscount)
    ) {
      updates.push({
        key: 'payment_setting.amount_discount',
        value: sanitized.AmountDiscount,
      })
    }

    if (updates.length === 0) {
      return
    }

    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }
    initialRef.current = { ...initialRef.current, ...sanitized }
    for (const key of Object.keys(sanitized) as Array<keyof typeof sanitized>) {
      form.resetField(key, { defaultValue: values[key] })
    }
  }

  const saveEpaySettings = async () => {
    const values = form.getValues()
    const sanitized = {
      PayAddress: removeTrailingSlash(values.PayAddress),
      EpayId: values.EpayId.trim(),
      EpayKey: values.EpayKey.trim(),
      CustomCallbackAddress: removeTrailingSlash(values.CustomCallbackAddress),
    }

    const initial = {
      PayAddress: removeTrailingSlash(initialRef.current.PayAddress),
      EpayId: initialRef.current.EpayId.trim(),
      EpayKey: initialRef.current.EpayKey.trim(),
      CustomCallbackAddress: removeTrailingSlash(
        initialRef.current.CustomCallbackAddress
      ),
    }

    const updates: Array<{ key: string; value: string }> = []

    if (sanitized.PayAddress !== initial.PayAddress) {
      updates.push({ key: 'PayAddress', value: sanitized.PayAddress })
    }

    if (sanitized.EpayId !== initial.EpayId) {
      updates.push({ key: 'EpayId', value: sanitized.EpayId })
    }

    if (sanitized.EpayKey && sanitized.EpayKey !== initial.EpayKey) {
      updates.push({ key: 'EpayKey', value: sanitized.EpayKey })
    }

    if (sanitized.CustomCallbackAddress !== initial.CustomCallbackAddress) {
      updates.push({
        key: 'CustomCallbackAddress',
        value: sanitized.CustomCallbackAddress,
      })
    }

    if (updates.length === 0) {
      return
    }

    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }
    initialRef.current = { ...initialRef.current, ...sanitized }
    for (const key of Object.keys(sanitized) as Array<keyof typeof sanitized>) {
      form.resetField(key, { defaultValue: values[key] })
    }
  }

  const saveStripeSettings = async () => {
    const values = form.getValues()
    const sanitized = {
      StripeApiSecret: values.StripeApiSecret.trim(),
      StripeWebhookSecret: values.StripeWebhookSecret.trim(),
      StripePriceId: values.StripePriceId.trim(),
      StripeCNYUnitPrice: values.StripeCNYUnitPrice as number,
      StripeHKDUnitPrice: values.StripeHKDUnitPrice as number,
      StripeUSDUnitPrice: values.StripeUSDUnitPrice as number,
      StripeUnitPrice: values.StripeUnitPrice as number,
      StripeMinTopUp: values.StripeMinTopUp as number,
      StripePromotionCodesEnabled:
        values.StripePromotionCodesEnabled as boolean,
    }

    const initial = {
      StripeApiSecret: initialRef.current.StripeApiSecret.trim(),
      StripeWebhookSecret: initialRef.current.StripeWebhookSecret.trim(),
      StripePriceId: initialRef.current.StripePriceId.trim(),
      StripeCNYUnitPrice: initialRef.current.StripeCNYUnitPrice,
      StripeHKDUnitPrice: initialRef.current.StripeHKDUnitPrice,
      StripeUSDUnitPrice: initialRef.current.StripeUSDUnitPrice,
      StripeUnitPrice: initialRef.current.StripeUnitPrice,
      StripeMinTopUp: initialRef.current.StripeMinTopUp,
      StripePromotionCodesEnabled:
        initialRef.current.StripePromotionCodesEnabled,
    }

    const updates: Array<{ key: string; value: string | number | boolean }> = []

    if (
      sanitized.StripeApiSecret &&
      sanitized.StripeApiSecret !== initial.StripeApiSecret
    ) {
      updates.push({ key: 'StripeApiSecret', value: sanitized.StripeApiSecret })
    }

    if (
      sanitized.StripeWebhookSecret &&
      sanitized.StripeWebhookSecret !== initial.StripeWebhookSecret
    ) {
      updates.push({
        key: 'StripeWebhookSecret',
        value: sanitized.StripeWebhookSecret,
      })
    }

    if (sanitized.StripePriceId !== initial.StripePriceId) {
      updates.push({ key: 'StripePriceId', value: sanitized.StripePriceId })
    }

    if (sanitized.StripeCNYUnitPrice !== initial.StripeCNYUnitPrice) {
      updates.push({
        key: 'StripeCNYUnitPrice',
        value: sanitized.StripeCNYUnitPrice,
      })
    }
    if (sanitized.StripeHKDUnitPrice !== initial.StripeHKDUnitPrice) {
      updates.push({
        key: 'StripeHKDUnitPrice',
        value: sanitized.StripeHKDUnitPrice,
      })
    }
    if (sanitized.StripeUSDUnitPrice !== initial.StripeUSDUnitPrice) {
      updates.push({
        key: 'StripeUSDUnitPrice',
        value: sanitized.StripeUSDUnitPrice,
      })
    }
    if (sanitized.StripeUnitPrice !== initial.StripeUnitPrice) {
      updates.push({ key: 'StripeUnitPrice', value: sanitized.StripeUnitPrice })
    }

    if (sanitized.StripeMinTopUp !== initial.StripeMinTopUp) {
      updates.push({ key: 'StripeMinTopUp', value: sanitized.StripeMinTopUp })
    }

    if (
      sanitized.StripePromotionCodesEnabled !==
      initial.StripePromotionCodesEnabled
    ) {
      updates.push({
        key: 'StripePromotionCodesEnabled',
        value: sanitized.StripePromotionCodesEnabled,
      })
    }

    if (updates.length === 0) {
      return
    }

    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }
    initialRef.current = { ...initialRef.current, ...sanitized }
    for (const key of Object.keys(sanitized) as Array<keyof typeof sanitized>) {
      form.resetField(key, { defaultValue: values[key] })
    }
  }

  const saveCreemSettings = async () => {
    const values = form.getValues()
    const sanitized = {
      CreemApiKey: values.CreemApiKey.trim(),
      CreemWebhookSecret: values.CreemWebhookSecret.trim(),
      CreemTestMode: values.CreemTestMode as boolean,
      CreemProducts: values.CreemProducts.trim(),
    }

    const initial = {
      CreemApiKey: initialRef.current.CreemApiKey.trim(),
      CreemWebhookSecret: initialRef.current.CreemWebhookSecret.trim(),
      CreemTestMode: initialRef.current.CreemTestMode,
      CreemProducts: initialRef.current.CreemProducts.trim(),
    }

    const updates: Array<{ key: string; value: string | boolean }> = []

    if (
      sanitized.CreemApiKey &&
      sanitized.CreemApiKey !== initial.CreemApiKey
    ) {
      updates.push({ key: 'CreemApiKey', value: sanitized.CreemApiKey })
    }

    if (
      sanitized.CreemWebhookSecret &&
      sanitized.CreemWebhookSecret !== initial.CreemWebhookSecret
    ) {
      updates.push({
        key: 'CreemWebhookSecret',
        value: sanitized.CreemWebhookSecret,
      })
    }

    if (sanitized.CreemTestMode !== initial.CreemTestMode) {
      updates.push({ key: 'CreemTestMode', value: sanitized.CreemTestMode })
    }

    if (
      normalizeJsonForComparison(sanitized.CreemProducts) !==
      normalizeJsonForComparison(initial.CreemProducts)
    ) {
      updates.push({ key: 'CreemProducts', value: sanitized.CreemProducts })
    }

    if (updates.length === 0) {
      return
    }

    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }
    initialRef.current = { ...initialRef.current, ...sanitized }
    for (const key of Object.keys(sanitized) as Array<keyof typeof sanitized>) {
      form.resetField(key, { defaultValue: values[key] })
    }
  }

  const saveXunhuSettings = async () => {
    const values = form.getValues()
    const sanitized = {
      XunhuEnabled: values.XunhuEnabled as boolean,
      XunhuAppID: values.XunhuAppID.trim(),
      XunhuSecret: values.XunhuSecret.trim(),
      XunhuGateway: removeTrailingSlash(values.XunhuGateway),
      XunhuMinTopUp: values.XunhuMinTopUp as number,
    }

    const initial = {
      XunhuEnabled: initialRef.current.XunhuEnabled,
      XunhuAppID: initialRef.current.XunhuAppID.trim(),
      XunhuSecret: initialRef.current.XunhuSecret.trim(),
      XunhuGateway: removeTrailingSlash(initialRef.current.XunhuGateway),
      XunhuMinTopUp: initialRef.current.XunhuMinTopUp,
    }

    const updates: Array<{ key: string; value: string | number | boolean }> = []

    if (sanitized.XunhuEnabled !== initial.XunhuEnabled) {
      updates.push({ key: 'XunhuEnabled', value: sanitized.XunhuEnabled })
    }
    if (sanitized.XunhuAppID !== initial.XunhuAppID) {
      updates.push({ key: 'XunhuAppID', value: sanitized.XunhuAppID })
    }
    if (
      sanitized.XunhuSecret &&
      sanitized.XunhuSecret !== initial.XunhuSecret
    ) {
      updates.push({ key: 'XunhuSecret', value: sanitized.XunhuSecret })
    }
    if (sanitized.XunhuGateway !== initial.XunhuGateway) {
      updates.push({ key: 'XunhuGateway', value: sanitized.XunhuGateway })
    }
    if (sanitized.XunhuMinTopUp !== initial.XunhuMinTopUp) {
      updates.push({ key: 'XunhuMinTopUp', value: sanitized.XunhuMinTopUp })
    }

    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }
    initialRef.current = { ...initialRef.current, ...sanitized }
    for (const key of Object.keys(sanitized) as Array<keyof typeof sanitized>) {
      form.resetField(key, { defaultValue: values[key] })
    }
  }

  const saveNowPaymentsSettings = async () => {
    const values = form.getValues()
    const sanitized = {
      NowPaymentsEnabled: values.NowPaymentsEnabled as boolean,
      NowPaymentsApiKey: values.NowPaymentsApiKey.trim(),
      NowPaymentsIPNSecret: values.NowPaymentsIPNSecret.trim(),
      NowPaymentsPaymentCurrency:
        values.NowPaymentsPaymentCurrency.trim().toLowerCase(),
      NowPaymentsPayCurrency:
        values.NowPaymentsPayCurrency.trim().toLowerCase(),
      NowPaymentsQuotaPerUSDT: values.NowPaymentsQuotaPerUSDT as number,
      NowPaymentsMinTopUp: values.NowPaymentsMinTopUp as number,
    }
    const initial = {
      NowPaymentsEnabled: initialRef.current.NowPaymentsEnabled,
      NowPaymentsApiKey: initialRef.current.NowPaymentsApiKey.trim(),
      NowPaymentsIPNSecret: initialRef.current.NowPaymentsIPNSecret.trim(),
      NowPaymentsPaymentCurrency:
        initialRef.current.NowPaymentsPaymentCurrency.trim().toLowerCase(),
      NowPaymentsPayCurrency:
        initialRef.current.NowPaymentsPayCurrency.trim().toLowerCase(),
      NowPaymentsQuotaPerUSDT: initialRef.current.NowPaymentsQuotaPerUSDT,
      NowPaymentsMinTopUp: initialRef.current.NowPaymentsMinTopUp,
    }
    const updates: Array<{ key: string; value: string | number | boolean }> = []
    if (sanitized.NowPaymentsEnabled !== initial.NowPaymentsEnabled) {
      updates.push({
        key: 'NowPaymentsEnabled',
        value: sanitized.NowPaymentsEnabled,
      })
    }
    if (
      sanitized.NowPaymentsApiKey &&
      sanitized.NowPaymentsApiKey !== initial.NowPaymentsApiKey
    ) {
      updates.push({
        key: 'NowPaymentsApiKey',
        value: sanitized.NowPaymentsApiKey,
      })
    }
    if (
      sanitized.NowPaymentsIPNSecret &&
      sanitized.NowPaymentsIPNSecret !== initial.NowPaymentsIPNSecret
    ) {
      updates.push({
        key: 'NowPaymentsIPNSecret',
        value: sanitized.NowPaymentsIPNSecret,
      })
    }
    if (
      sanitized.NowPaymentsPaymentCurrency !==
      initial.NowPaymentsPaymentCurrency
    ) {
      updates.push({
        key: 'NowPaymentsPaymentCurrency',
        value: sanitized.NowPaymentsPaymentCurrency,
      })
    }
    if (sanitized.NowPaymentsPayCurrency !== initial.NowPaymentsPayCurrency) {
      updates.push({
        key: 'NowPaymentsPayCurrency',
        value: sanitized.NowPaymentsPayCurrency,
      })
    }
    if (sanitized.NowPaymentsQuotaPerUSDT !== initial.NowPaymentsQuotaPerUSDT) {
      updates.push({
        key: 'NowPaymentsQuotaPerUSDT',
        value: sanitized.NowPaymentsQuotaPerUSDT,
      })
    }
    if (sanitized.NowPaymentsMinTopUp !== initial.NowPaymentsMinTopUp) {
      updates.push({
        key: 'NowPaymentsMinTopUp',
        value: sanitized.NowPaymentsMinTopUp,
      })
    }
    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }
    initialRef.current = { ...initialRef.current, ...sanitized }
    for (const key of Object.keys(sanitized) as Array<keyof typeof sanitized>) {
      form.resetField(key, { defaultValue: values[key] })
    }
  }

  const onSubmit = async (values: PaymentFormValues) => {
    const sanitized = {
      PayAddress: removeTrailingSlash(values.PayAddress),
      EpayId: values.EpayId.trim(),
      EpayKey: values.EpayKey.trim(),
      Price: values.Price,
      MinTopUp: values.MinTopUp,
      CustomCallbackAddress: removeTrailingSlash(values.CustomCallbackAddress),
      PayMethods: values.PayMethods.trim(),
      AmountOptions: values.AmountOptions.trim(),
      AmountDiscount: values.AmountDiscount.trim(),
      StripeApiSecret: values.StripeApiSecret.trim(),
      StripeWebhookSecret: values.StripeWebhookSecret.trim(),
      StripePriceId: values.StripePriceId.trim(),
      StripeCNYUnitPrice: values.StripeCNYUnitPrice,
      StripeHKDUnitPrice: values.StripeHKDUnitPrice,
      StripeUSDUnitPrice: values.StripeUSDUnitPrice,
      StripeUnitPrice: values.StripeUnitPrice,
      StripeMinTopUp: values.StripeMinTopUp,
      StripePromotionCodesEnabled: values.StripePromotionCodesEnabled,
      XunhuEnabled: values.XunhuEnabled,
      XunhuAppID: values.XunhuAppID.trim(),
      XunhuSecret: values.XunhuSecret.trim(),
      XunhuGateway: removeTrailingSlash(values.XunhuGateway),
      XunhuMinTopUp: values.XunhuMinTopUp,
      NowPaymentsEnabled: values.NowPaymentsEnabled,
      NowPaymentsApiKey: values.NowPaymentsApiKey.trim(),
      NowPaymentsIPNSecret: values.NowPaymentsIPNSecret.trim(),
      NowPaymentsPaymentCurrency:
        values.NowPaymentsPaymentCurrency.trim().toLowerCase(),
      NowPaymentsPayCurrency:
        values.NowPaymentsPayCurrency.trim().toLowerCase(),
      NowPaymentsQuotaPerUSDT: values.NowPaymentsQuotaPerUSDT,
      NowPaymentsMinTopUp: values.NowPaymentsMinTopUp,
    }

    const initial = {
      PayAddress: removeTrailingSlash(initialRef.current.PayAddress),
      EpayId: initialRef.current.EpayId.trim(),
      EpayKey: initialRef.current.EpayKey.trim(),
      Price: initialRef.current.Price,
      MinTopUp: initialRef.current.MinTopUp,
      CustomCallbackAddress: removeTrailingSlash(
        initialRef.current.CustomCallbackAddress
      ),
      PayMethods: initialRef.current.PayMethods.trim(),
      AmountOptions: initialRef.current.AmountOptions.trim(),
      AmountDiscount: initialRef.current.AmountDiscount.trim(),
      StripeApiSecret: initialRef.current.StripeApiSecret.trim(),
      StripeWebhookSecret: initialRef.current.StripeWebhookSecret.trim(),
      StripePriceId: initialRef.current.StripePriceId.trim(),
      StripeCNYUnitPrice: initialRef.current.StripeCNYUnitPrice,
      StripeHKDUnitPrice: initialRef.current.StripeHKDUnitPrice,
      StripeUSDUnitPrice: initialRef.current.StripeUSDUnitPrice,
      StripeUnitPrice: initialRef.current.StripeUnitPrice,
      StripeMinTopUp: initialRef.current.StripeMinTopUp,
      StripePromotionCodesEnabled:
        initialRef.current.StripePromotionCodesEnabled,
      XunhuEnabled: initialRef.current.XunhuEnabled,
      XunhuAppID: initialRef.current.XunhuAppID.trim(),
      XunhuSecret: initialRef.current.XunhuSecret.trim(),
      XunhuGateway: removeTrailingSlash(initialRef.current.XunhuGateway),
      XunhuMinTopUp: initialRef.current.XunhuMinTopUp,
      NowPaymentsEnabled: initialRef.current.NowPaymentsEnabled,
      NowPaymentsApiKey: initialRef.current.NowPaymentsApiKey.trim(),
      NowPaymentsIPNSecret: initialRef.current.NowPaymentsIPNSecret.trim(),
      NowPaymentsPaymentCurrency:
        initialRef.current.NowPaymentsPaymentCurrency.trim().toLowerCase(),
      NowPaymentsPayCurrency:
        initialRef.current.NowPaymentsPayCurrency.trim().toLowerCase(),
      NowPaymentsQuotaPerUSDT: initialRef.current.NowPaymentsQuotaPerUSDT,
      NowPaymentsMinTopUp: initialRef.current.NowPaymentsMinTopUp,
    }

    const updates: Array<{ key: string; value: string | number | boolean }> = []

    if (sanitized.NowPaymentsEnabled !== initial.NowPaymentsEnabled) {
      updates.push({
        key: 'NowPaymentsEnabled',
        value: sanitized.NowPaymentsEnabled,
      })
    }
    if (
      sanitized.NowPaymentsApiKey &&
      sanitized.NowPaymentsApiKey !== initial.NowPaymentsApiKey
    ) {
      updates.push({
        key: 'NowPaymentsApiKey',
        value: sanitized.NowPaymentsApiKey,
      })
    }
    if (
      sanitized.NowPaymentsIPNSecret &&
      sanitized.NowPaymentsIPNSecret !== initial.NowPaymentsIPNSecret
    ) {
      updates.push({
        key: 'NowPaymentsIPNSecret',
        value: sanitized.NowPaymentsIPNSecret,
      })
    }
    if (
      sanitized.NowPaymentsPaymentCurrency !==
      initial.NowPaymentsPaymentCurrency
    ) {
      updates.push({
        key: 'NowPaymentsPaymentCurrency',
        value: sanitized.NowPaymentsPaymentCurrency,
      })
    }
    if (sanitized.NowPaymentsPayCurrency !== initial.NowPaymentsPayCurrency) {
      updates.push({
        key: 'NowPaymentsPayCurrency',
        value: sanitized.NowPaymentsPayCurrency,
      })
    }
    if (sanitized.NowPaymentsQuotaPerUSDT !== initial.NowPaymentsQuotaPerUSDT) {
      updates.push({
        key: 'NowPaymentsQuotaPerUSDT',
        value: sanitized.NowPaymentsQuotaPerUSDT,
      })
    }
    if (sanitized.NowPaymentsMinTopUp !== initial.NowPaymentsMinTopUp) {
      updates.push({
        key: 'NowPaymentsMinTopUp',
        value: sanitized.NowPaymentsMinTopUp,
      })
    }

    if (sanitized.PayAddress !== initial.PayAddress) {
      updates.push({ key: 'PayAddress', value: sanitized.PayAddress })
    }

    if (sanitized.EpayId !== initial.EpayId) {
      updates.push({ key: 'EpayId', value: sanitized.EpayId })
    }

    if (sanitized.EpayKey && sanitized.EpayKey !== initial.EpayKey) {
      updates.push({ key: 'EpayKey', value: sanitized.EpayKey })
    }

    if (sanitized.Price !== initial.Price) {
      updates.push({ key: 'Price', value: sanitized.Price })
    }

    if (sanitized.MinTopUp !== initial.MinTopUp) {
      updates.push({ key: 'MinTopUp', value: sanitized.MinTopUp })
    }

    if (sanitized.CustomCallbackAddress !== initial.CustomCallbackAddress) {
      updates.push({
        key: 'CustomCallbackAddress',
        value: sanitized.CustomCallbackAddress,
      })
    }

    if (
      normalizeJsonForComparison(sanitized.PayMethods) !==
      normalizeJsonForComparison(initial.PayMethods)
    ) {
      updates.push({ key: 'PayMethods', value: sanitized.PayMethods })
    }

    if (
      normalizeJsonForComparison(sanitized.AmountOptions) !==
      normalizeJsonForComparison(initial.AmountOptions)
    ) {
      updates.push({
        key: 'payment_setting.amount_options',
        value: sanitized.AmountOptions,
      })
    }

    if (
      normalizeJsonForComparison(sanitized.AmountDiscount) !==
      normalizeJsonForComparison(initial.AmountDiscount)
    ) {
      updates.push({
        key: 'payment_setting.amount_discount',
        value: sanitized.AmountDiscount,
      })
    }

    if (
      sanitized.StripeApiSecret &&
      sanitized.StripeApiSecret !== initial.StripeApiSecret
    ) {
      updates.push({ key: 'StripeApiSecret', value: sanitized.StripeApiSecret })
    }

    if (
      sanitized.StripeWebhookSecret &&
      sanitized.StripeWebhookSecret !== initial.StripeWebhookSecret
    ) {
      updates.push({
        key: 'StripeWebhookSecret',
        value: sanitized.StripeWebhookSecret,
      })
    }

    if (sanitized.StripePriceId !== initial.StripePriceId) {
      updates.push({ key: 'StripePriceId', value: sanitized.StripePriceId })
    }

    if (sanitized.StripeCNYUnitPrice !== initial.StripeCNYUnitPrice) {
      updates.push({
        key: 'StripeCNYUnitPrice',
        value: sanitized.StripeCNYUnitPrice,
      })
    }
    if (sanitized.StripeHKDUnitPrice !== initial.StripeHKDUnitPrice) {
      updates.push({
        key: 'StripeHKDUnitPrice',
        value: sanitized.StripeHKDUnitPrice,
      })
    }
    if (sanitized.StripeUSDUnitPrice !== initial.StripeUSDUnitPrice) {
      updates.push({
        key: 'StripeUSDUnitPrice',
        value: sanitized.StripeUSDUnitPrice,
      })
    }
    if (sanitized.StripeUnitPrice !== initial.StripeUnitPrice) {
      updates.push({ key: 'StripeUnitPrice', value: sanitized.StripeUnitPrice })
    }

    if (sanitized.StripeMinTopUp !== initial.StripeMinTopUp) {
      updates.push({ key: 'StripeMinTopUp', value: sanitized.StripeMinTopUp })
    }

    if (
      sanitized.StripePromotionCodesEnabled !==
      initial.StripePromotionCodesEnabled
    ) {
      updates.push({
        key: 'StripePromotionCodesEnabled',
        value: sanitized.StripePromotionCodesEnabled,
      })
    }

    if (sanitized.XunhuEnabled !== initial.XunhuEnabled) {
      updates.push({ key: 'XunhuEnabled', value: sanitized.XunhuEnabled })
    }

    if (sanitized.XunhuAppID !== initial.XunhuAppID) {
      updates.push({ key: 'XunhuAppID', value: sanitized.XunhuAppID })
    }

    if (
      sanitized.XunhuSecret &&
      sanitized.XunhuSecret !== initial.XunhuSecret
    ) {
      updates.push({ key: 'XunhuSecret', value: sanitized.XunhuSecret })
    }

    if (sanitized.XunhuGateway !== initial.XunhuGateway) {
      updates.push({ key: 'XunhuGateway', value: sanitized.XunhuGateway })
    }

    if (sanitized.XunhuMinTopUp !== initial.XunhuMinTopUp) {
      updates.push({ key: 'XunhuMinTopUp', value: sanitized.XunhuMinTopUp })
    }

    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }
    initialRef.current = { ...initialRef.current, ...sanitized }
    for (const key of Object.keys(sanitized) as Array<keyof typeof sanitized>) {
      form.resetField(key, { defaultValue: values[key] })
    }
  }

  return (
    <SettingsSection
      title={t('Payment Gateway')}
      description={t('Configure payment gateway integrations')}
    >
      {!complianceConfirmed ? (
        <Alert variant='destructive' className='mb-6'>
          <ShieldAlert className='h-4 w-4' />
          <AlertTitle>{t('Compliance confirmation required')}</AlertTitle>
          <AlertDescription>
            <div className='space-y-3'>
              <p>
                {t(
                  'Payment, redemption codes, subscription plans, and invitation rewards are locked until the root administrator confirms the compliance terms.'
                )}
              </p>
              <ol className='list-decimal space-y-1 pl-5'>
                {complianceStatements.map((statement) => (
                  <li key={statement}>{statement}</li>
                ))}
              </ol>
            </div>
          </AlertDescription>
          <AlertAction>
            <Button
              type='button'
              size='sm'
              variant='destructive'
              onClick={() => setShowComplianceDialog(true)}
            >
              {t('Confirm compliance')}
            </Button>
          </AlertAction>
        </Alert>
      ) : (
        <Alert className='mb-6'>
          <AlertTitle>{t('Compliance confirmed')}</AlertTitle>
          <AlertDescription>
            {t('Confirmed at {{time}} by user #{{userId}}', {
              time: complianceDefaults.confirmedAt
                ? new Date(
                    complianceDefaults.confirmedAt * 1000
                  ).toLocaleString(getIntlLocale())
                : '-',
              userId: complianceDefaults.confirmedBy || '-',
            })}
          </AlertDescription>
        </Alert>
      )}

      <RiskAcknowledgementDialog
        open={showComplianceDialog}
        onOpenChange={setShowComplianceDialog}
        title={t('Confirm compliance terms')}
        description={t(
          'This confirmation unlocks payment, redemption code, subscription plan, and invitation reward features. Please read the statements carefully.'
        )}
        items={complianceStatements}
        requiredText={complianceRequiredText}
        requiredTextParts={complianceRequiredTextParts}
        inputPrompt={t('Please type the following text to confirm:')}
        inputPlaceholder={t('Type the confirmation text here')}
        mismatchHint={t('The entered text does not match the required text.')}
        confirmText={t('Confirm and enable')}
        isLoading={confirmComplianceMutation.isPending}
        onConfirm={() => confirmComplianceMutation.mutate()}
      />

      {/* eslint-disable react-hooks/refs */}
      <Form {...form}>
        <form
          onSubmit={handleSettingsAction(form.handleSubmit(onSubmit))}
          className={cn(
            'space-y-8',
            !complianceConfirmed && 'pointer-events-none opacity-40'
          )}
          data-no-autosubmit='true'
        >
          <div className='space-y-4'>
            <div>
              <h3 className='text-lg font-medium'>{t('General Settings')}</h3>
            </div>

            <div className='grid gap-6 md:grid-cols-2'>
              <FormField
                control={form.control}
                name='Price'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Price (local currency / USD)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        step='0.01'
                        min={0}
                        value={(field.value ?? 0) as number}
                        onChange={(event) =>
                          field.onChange(event.target.valueAsNumber)
                        }
                      />
                    </FormControl>

                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='MinTopUp'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Minimum topup amount')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        step='0.01'
                        min={0}
                        value={(field.value ?? 0) as number}
                        onChange={(event) =>
                          field.onChange(event.target.valueAsNumber)
                        }
                      />
                    </FormControl>

                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>

            <FormField
              control={form.control}
              name='PayMethods'
              render={({ field }) => (
                <FormItem>
                  <div className='mb-2 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
                    <FormLabel>{t('Payment methods')}</FormLabel>
                    <Button
                      type='button'
                      variant='outline'
                      size='sm'
                      onClick={() =>
                        setPayMethodsVisualMode(!payMethodsVisualMode)
                      }
                      className='w-full sm:w-auto'
                    >
                      {payMethodsVisualMode ? (
                        <>
                          <Code2 className='mr-2 h-3 w-3' />
                          {t('JSON Editor')}
                        </>
                      ) : (
                        <>
                          <Eye className='mr-2 h-3 w-3' />
                          {t('Visual Editor')}
                        </>
                      )}
                    </Button>
                  </div>
                  <FormControl>
                    {payMethodsVisualMode ? (
                      <PaymentMethodsVisualEditor
                        value={field.value}
                        onChange={field.onChange}
                      />
                    ) : (
                      <Textarea
                        rows={4}
                        placeholder={t(
                          '[{"name":"支付宝","type":"alipay","color":"#1677FF"}]'
                        )}
                        {...field}
                        onChange={(event) => field.onChange(event.target.value)}
                      />
                    )}
                  </FormControl>
                  <FormDescription className='settings-field-fact'>
                    {t('JSON []')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <div className='grid gap-6 md:grid-cols-2 md:items-start'>
              <FormField
                control={form.control}
                name='AmountOptions'
                render={({ field }) => (
                  <FormItem>
                    <div className='mb-2 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
                      <FormLabel>{t('Top-up amount options')}</FormLabel>
                      <Button
                        type='button'
                        variant='outline'
                        size='sm'
                        onClick={() =>
                          setAmountOptionsVisualMode(!amountOptionsVisualMode)
                        }
                        className='w-full sm:w-auto'
                      >
                        {amountOptionsVisualMode ? (
                          <>
                            <Code2 className='mr-2 h-3 w-3' />
                            {t('JSON Editor')}
                          </>
                        ) : (
                          <>
                            <Eye className='mr-2 h-3 w-3' />
                            {t('Visual Editor')}
                          </>
                        )}
                      </Button>
                    </div>
                    <FormControl>
                      {amountOptionsVisualMode ? (
                        <AmountOptionsVisualEditor
                          value={field.value}
                          onChange={field.onChange}
                        />
                      ) : (
                        <Textarea
                          rows={4}
                          placeholder='[10, 20, 50, 100]'
                          {...field}
                          onChange={(event) =>
                            field.onChange(event.target.value)
                          }
                        />
                      )}
                    </FormControl>
                    <FormDescription className='settings-field-fact'>
                      {t('JSON []')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='AmountDiscount'
                render={({ field }) => (
                  <FormItem>
                    <div className='mb-2 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
                      <FormLabel>{t('Amount discount')}</FormLabel>
                      <Button
                        type='button'
                        variant='outline'
                        size='sm'
                        onClick={() =>
                          setAmountDiscountVisualMode(!amountDiscountVisualMode)
                        }
                        className='w-full sm:w-auto'
                      >
                        {amountDiscountVisualMode ? (
                          <>
                            <Code2 className='mr-2 h-3 w-3' />
                            {t('JSON Editor')}
                          </>
                        ) : (
                          <>
                            <Eye className='mr-2 h-3 w-3' />
                            {t('Visual Editor')}
                          </>
                        )}
                      </Button>
                    </div>
                    <FormControl>
                      {amountDiscountVisualMode ? (
                        <AmountDiscountVisualEditor
                          value={field.value}
                          onChange={field.onChange}
                        />
                      ) : (
                        <Textarea
                          rows={4}
                          placeholder='{"100":0.95,"200":0.9}'
                          {...field}
                          onChange={(event) =>
                            field.onChange(event.target.value)
                          }
                        />
                      )}
                    </FormControl>
                    <FormDescription className='settings-field-fact'>
                      {t('JSON {}')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>

            <Button
              type='button'
              onClick={(e) => {
                e.preventDefault()
                e.stopPropagation()
                void handleSettingsAction(saveGeneralSettings)()
              }}
              disabled={updateOption.isPending}
            >
              {updateOption.isPending
                ? t('Saving...')
                : t('Save general settings')}
            </Button>
          </div>

          <Separator />

          <div className='space-y-4'>
            <div>
              <h3 className='text-lg font-medium'>{t('Epay Gateway')}</h3>
            </div>

            <div className='grid gap-6 md:grid-cols-2'>
              <FormField
                control={form.control}
                name='PayAddress'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Epay endpoint')}</FormLabel>
                    <FormControl>
                      <Input
                        placeholder={t('https://pay.example.com')}
                        {...field}
                        onChange={(event) => field.onChange(event.target.value)}
                      />
                    </FormControl>

                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='CustomCallbackAddress'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Callback address')}</FormLabel>
                    <FormControl>
                      <Input
                        placeholder={t('https://gateway.example.com')}
                        {...field}
                        onChange={(event) => field.onChange(event.target.value)}
                      />
                    </FormControl>
                    <FormDescription className='settings-field-fact'>
                      {t('Empty: server address')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>

            <div className='grid gap-6 md:grid-cols-2'>
              <FormField
                control={form.control}
                name='EpayId'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Epay merchant ID')}</FormLabel>
                    <FormControl>
                      <Input
                        placeholder='10001'
                        autoComplete='off'
                        {...field}
                        onChange={(event) => field.onChange(event.target.value)}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='EpayKey'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Epay secret key')}</FormLabel>
                    <FormControl>
                      <Input
                        type='password'
                        autoComplete='new-password'
                        {...field}
                        onChange={(event) => field.onChange(event.target.value)}
                      />
                    </FormControl>
                    <FormDescription className='settings-field-fact'>
                      {t('Empty: unchanged')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>

            <Button
              type='button'
              onClick={(e) => {
                e.preventDefault()
                e.stopPropagation()
                void handleSettingsAction(saveEpaySettings)()
              }}
              disabled={updateOption.isPending}
            >
              {updateOption.isPending
                ? t('Saving...')
                : t('Save Epay settings')}
            </Button>
          </div>

          <Separator />

          <div className='space-y-4'>
            <div>
              <h3 className='text-lg font-medium'>{t('Stripe Gateway')}</h3>
            </div>

            <div className='rounded-md bg-blue-50 p-4 text-sm text-blue-900 dark:bg-blue-950 dark:text-blue-100'>
              <p className='mb-2 font-medium'>{t('Webhook Configuration:')}</p>
              <ul className='list-inside list-disc space-y-1'>
                <li>
                  {t('Webhook URL:')}{' '}
                  <code className='rounded bg-blue-100 px-1 py-0.5 text-xs dark:bg-blue-900'>
                    {'<ServerAddress>/api/stripe/webhook'}
                  </code>
                </li>
                <li>
                  {t('Required events:')}{' '}
                  <code className='rounded bg-blue-100 px-1 py-0.5 text-xs dark:bg-blue-900'>
                    checkout.session.completed
                  </code>{' '}
                  {t('and')}{' '}
                  <code className='rounded bg-blue-100 px-1 py-0.5 text-xs dark:bg-blue-900'>
                    checkout.session.expired,
                    checkout.session.async_payment_succeeded,
                    checkout.session.async_payment_failed
                  </code>
                </li>
                <li>
                  {t('Configure at:')}{' '}
                  <a
                    href='https://dashboard.stripe.com/developers'
                    target='_blank'
                    rel='noreferrer'
                    className='underline hover:no-underline'
                  >
                    {t('Stripe Dashboard')}
                  </a>
                </li>
              </ul>
            </div>

            <div className='grid gap-6 md:grid-cols-3'>
              <FormField
                control={form.control}
                name='StripeApiSecret'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('API secret')}</FormLabel>
                    <FormControl>
                      <Input
                        type='password'
                        placeholder={t('sk_xxx or rk_xxx')}
                        autoComplete='new-password'
                        {...field}
                        onChange={(event) => field.onChange(event.target.value)}
                      />
                    </FormControl>
                    <FormDescription className='settings-field-fact'>
                      {t('Empty: unchanged')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='StripeWebhookSecret'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Webhook secret')}</FormLabel>
                    <FormControl>
                      <Input
                        type='password'
                        placeholder={t('whsec_xxx')}
                        autoComplete='new-password'
                        {...field}
                        onChange={(event) => field.onChange(event.target.value)}
                      />
                    </FormControl>
                    <FormDescription className='settings-field-fact'>
                      {t('Empty: unchanged')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>

            <div className='grid gap-6 md:grid-cols-3'>
              <FormField
                control={form.control}
                name='StripeHKDUnitPrice'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('HKD / credit')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={0.01}
                        step='0.01'
                        value={field.value as number}
                        onChange={(event) =>
                          field.onChange(event.target.valueAsNumber)
                        }
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='StripeUSDUnitPrice'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('USD / credit')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={0.01}
                        step='0.01'
                        value={field.value as number}
                        onChange={(event) =>
                          field.onChange(event.target.valueAsNumber)
                        }
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='StripeCNYUnitPrice'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('CNY / credit')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={0.01}
                        step='0.01'
                        value={field.value as number}
                        onChange={(event) =>
                          field.onChange(event.target.valueAsNumber)
                        }
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='StripeMinTopUp'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Minimum topup amount')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        step='1'
                        min={5}
                        value={(field.value ?? 0) as number}
                        onChange={(event) =>
                          field.onChange(event.target.valueAsNumber)
                        }
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>

            <Button
              type='button'
              onClick={(e) => {
                e.preventDefault()
                e.stopPropagation()
                void handleSettingsAction(saveStripeSettings)()
              }}
              disabled={updateOption.isPending}
            >
              {updateOption.isPending
                ? t('Saving...')
                : t('Save Stripe settings')}
            </Button>
          </div>

          <Separator />

          <div className='space-y-4'>
            <div>
              <h3 className='text-lg font-medium'>{t('Creem Gateway')}</h3>
            </div>

            <div className='rounded-md bg-blue-50 p-4 text-sm text-blue-900 dark:bg-blue-950 dark:text-blue-100'>
              <p className='mb-2 font-medium'>{t('Webhook Configuration:')}</p>
              <ul className='list-inside list-disc space-y-1'>
                <li>
                  {t('Webhook URL:')}{' '}
                  <code className='rounded bg-blue-100 px-1 py-0.5 text-xs dark:bg-blue-900'>
                    {'<ServerAddress>/api/creem/webhook'}
                  </code>
                </li>
                <li>{t('Configure in your Creem dashboard')}</li>
              </ul>
            </div>

            <div className='grid gap-6 md:grid-cols-2'>
              <FormField
                control={form.control}
                name='CreemApiKey'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('API Key')}</FormLabel>
                    <FormControl>
                      <Input
                        type='password'
                        autoComplete='new-password'
                        {...field}
                        onChange={(event) => field.onChange(event.target.value)}
                      />
                    </FormControl>
                    <FormDescription className='settings-field-fact'>
                      {t('Empty: unchanged')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='CreemWebhookSecret'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Webhook Secret')}</FormLabel>
                    <FormControl>
                      <Input
                        type='password'
                        autoComplete='new-password'
                        {...field}
                        onChange={(event) => field.onChange(event.target.value)}
                      />
                    </FormControl>
                    <FormDescription className='settings-field-fact'>
                      {t('Empty: unchanged')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>

            <FormField
              control={form.control}
              name='CreemTestMode'
              render={({ field }) => (
                <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                  <div className='space-y-0.5'>
                    <FormLabel className='text-base'>
                      {t('Test Mode')}
                    </FormLabel>
                  </div>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='CreemProducts'
              render={({ field }) => (
                <FormItem>
                  <div className='mb-2 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
                    <FormLabel>{t('Products')}</FormLabel>
                    <Button
                      type='button'
                      variant='outline'
                      size='sm'
                      onClick={() =>
                        setCreemProductsVisualMode(!creemProductsVisualMode)
                      }
                      className='w-full sm:w-auto'
                    >
                      {creemProductsVisualMode ? (
                        <>
                          <Code2 className='mr-2 h-3 w-3' />
                          {t('JSON Editor')}
                        </>
                      ) : (
                        <>
                          <Eye className='mr-2 h-3 w-3' />
                          {t('Visual Editor')}
                        </>
                      )}
                    </Button>
                  </div>
                  <FormControl>
                    {creemProductsVisualMode ? (
                      <CreemProductsVisualEditor
                        value={field.value}
                        onChange={field.onChange}
                      />
                    ) : (
                      <Textarea
                        rows={4}
                        placeholder='[{"name":"Basic","productId":"prod_xxx","price":10,"quota":500000,"currency":"USD"}]'
                        {...field}
                        onChange={(event) => field.onChange(event.target.value)}
                      />
                    )}
                  </FormControl>
                  <FormDescription className='settings-field-fact'>
                    {t('JSON []')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <Button
              type='button'
              onClick={(e) => {
                e.preventDefault()
                e.stopPropagation()
                void handleSettingsAction(saveCreemSettings)()
              }}
              disabled={updateOption.isPending}
            >
              {updateOption.isPending
                ? t('Saving...')
                : t('Save Creem settings')}
            </Button>
          </div>

          <Separator />

          <div className='space-y-4'>
            <div>
              <h3 className='text-lg font-medium'>{t('WeChat Pay')}</h3>
            </div>

            <FormField
              control={form.control}
              name='XunhuEnabled'
              render={({ field }) => (
                <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                  <div className='space-y-0.5'>
                    <FormLabel className='text-base'>{t('Enabled')}</FormLabel>
                  </div>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </FormItem>
              )}
            />

            <div className='grid gap-6 md:grid-cols-2'>
              <FormField
                control={form.control}
                name='XunhuAppID'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('App ID')}</FormLabel>
                    <FormControl>
                      <Input {...field} placeholder='app_1234567890' />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='XunhuSecret'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Secret')}</FormLabel>
                    <FormControl>
                      <Input
                        type='password'
                        autoComplete='new-password'
                        {...field}
                      />
                    </FormControl>
                    <FormDescription className='settings-field-fact'>
                      {t('Empty: unchanged')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>

            <div className='grid gap-6 md:grid-cols-2'>
              <FormField
                control={form.control}
                name='XunhuGateway'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Gateway URL')}</FormLabel>
                    <FormControl>
                      <Input
                        {...field}
                        placeholder='https://api.xunhupay.com/payment/do.html'
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='XunhuMinTopUp'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Minimum Payment Amount (CNY)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={0}
                        value={(field.value ?? 0) as number}
                        onChange={(event) =>
                          field.onChange(event.target.valueAsNumber)
                        }
                      />
                    </FormControl>
                    <FormDescription>
                      {t('Minimum WeChat Pay charge in CNY')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>

            <Button
              type='button'
              onClick={(e) => {
                e.preventDefault()
                e.stopPropagation()
                void handleSettingsAction(saveXunhuSettings)()
              }}
              disabled={updateOption.isPending}
            >
              {updateOption.isPending
                ? t('Saving...')
                : t('Save WeChat Pay settings')}
            </Button>
          </div>

          <Separator />

          <div className='space-y-4'>
            <div>
              <h3 className='text-lg font-medium'>{t('NOWPayments USDT')}</h3>
              <p className='text-muted-foreground text-base'>
                {t('IPN URL:')} <code>/api/nowpayments/ipn</code>
              </p>
            </div>

            <FormField
              control={form.control}
              name='NowPaymentsEnabled'
              render={({ field }) => (
                <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                  <div className='space-y-0.5'>
                    <FormLabel className='text-base'>{t('Enabled')}</FormLabel>
                  </div>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </FormItem>
              )}
            />

            <div className='grid gap-6 md:grid-cols-2'>
              <FormField
                control={form.control}
                name='NowPaymentsApiKey'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('API Key')}</FormLabel>
                    <FormControl>
                      <Input
                        type='password'
                        autoComplete='new-password'
                        {...field}
                      />
                    </FormControl>
                    <FormDescription className='settings-field-fact'>
                      {t('Empty: unchanged')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='NowPaymentsIPNSecret'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('IPN Secret')}</FormLabel>
                    <FormControl>
                      <Input
                        type='password'
                        autoComplete='new-password'
                        {...field}
                      />
                    </FormControl>
                    <FormDescription className='settings-field-fact'>
                      {t('x-nowpayments-sig')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>

            <div className='grid gap-6 md:grid-cols-4'>
              <FormField
                control={form.control}
                name='NowPaymentsQuotaPerUSDT'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Quota per 1 USDT')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={0.000001}
                        step='0.1'
                        value={(field.value ?? 5) as number}
                        onChange={(event) =>
                          field.onChange(event.target.valueAsNumber)
                        }
                      />
                    </FormControl>
                    <FormDescription>{t('Default: 5 quotas')}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='NowPaymentsMinTopUp'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Minimum top-up quota')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={1}
                        step='1'
                        value={(field.value ?? 5) as number}
                        onChange={(event) =>
                          field.onChange(event.target.valueAsNumber)
                        }
                      />
                    </FormControl>
                    <FormDescription>{t('Default: 5 quotas')}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='NowPaymentsPaymentCurrency'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Payment currency')}</FormLabel>
                    <FormControl>
                      <Input readOnly {...field} value='USDT' />
                    </FormControl>
                    <FormDescription>{t('Fixed to USDT')}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='NowPaymentsPayCurrency'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('USDT network')}</FormLabel>
                    <FormControl>
                      <Input
                        placeholder='usdttrc20'
                        {...field}
                        onChange={(event) =>
                          field.onChange(event.target.value.toLowerCase())
                        }
                      />
                    </FormControl>
                    <FormDescription>{t('Default: USDTTRC20')}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>

            <Button
              type='button'
              onClick={(e) => {
                e.preventDefault()
                e.stopPropagation()
                void handleSettingsAction(saveNowPaymentsSettings)()
              }}
              disabled={updateOption.isPending}
            >
              {updateOption.isPending
                ? t('Saving...')
                : t('Save NOWPayments settings')}
            </Button>
          </div>

          <div className='settings-save-bar'>
            <Button type='submit' disabled={updateOption.isPending}>
              {updateOption.isPending ? t('Saving...') : t('Save all settings')}
            </Button>
          </div>
        </form>
      </Form>

      <Separator />

      <WaffoSettingsSection defaultValues={waffoDefaultValues} />

      <Separator />

      <WaffoPancakeSettingsSection defaultValues={waffoPancakeDefaultValues} />
      {/* eslint-enable react-hooks/refs */}
    </SettingsSection>
  )
}
