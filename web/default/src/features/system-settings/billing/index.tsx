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
import { SettingsPage } from '../components/settings-page'
import type { BillingSettings } from '../types'
import {
  BILLING_DEFAULT_SECTION,
  getBillingSectionContent,
} from './section-registry.tsx'

const defaultBillingSettings: BillingSettings = {
  QuotaForNewUser: 0,
  PreConsumedQuota: 0,
  QuotaForInviter: 0,
  QuotaForInvitee: 0,
  TopUpLink: '',
  'general_setting.docs_link': '',
  'quota_setting.enable_free_model_pre_consume': true,
  QuotaPerUnit: 500000,
  USDExchangeRate: 7,
  'general_setting.quota_display_type': 'USD',
  'general_setting.custom_currency_symbol': '陇',
  'general_setting.custom_currency_exchange_rate': 1,
  DisplayInCurrencyEnabled: true,
  DisplayTokenStatEnabled: true,
  ModelPrice: '',
  ModelRatio: '',
  CacheRatio: '',
  CreateCacheRatio: '',
  CompletionRatio: '',
  ImageRatio: '',
  AudioRatio: '',
  AudioCompletionRatio: '',
  ExposeRatioEnabled: false,
  'billing_setting.billing_mode': '{}',
  'billing_setting.billing_expr': '{}',
  'tool_price_setting.prices': '{}',
  TopupGroupRatio: '',
  GroupRatio: '',
  UserUsableGroups: '',
  GroupGroupRatio: '',
  AutoGroups: '',
  DefaultUseAutoGroup: false,
  'group_ratio_setting.group_special_usable_group': '{}',
  PayAddress: '',
  EpayId: '',
  EpayKey: '',
  Price: 7.3,
  MinTopUp: 1,
  CustomCallbackAddress: '',
  PayMethods: '',
  'payment_setting.amount_options': '',
  'payment_setting.amount_discount': '',
  'payment_setting.first_purchase_discount_enabled': false,
  'payment_setting.first_purchase_discount_multiplier': 0.8,
  'payment_setting.first_purchase_discount_start_at': 0,
  'payment_setting.first_purchase_discount_end_at': 0,
  'payment_setting.compliance_confirmed': false,
  'payment_setting.compliance_terms_version': '',
  'payment_setting.compliance_confirmed_at': 0,
  'payment_setting.compliance_confirmed_by': 0,
  'payment_setting.compliance_confirmed_ip': '',
  StripeApiSecret: '',
  StripeWebhookSecret: '',
  StripePriceId: '',
  StripeUnitPrice: 8.0,
  StripeMinTopUp: 1,
  StripePromotionCodesEnabled: false,
  CreemApiKey: '',
  CreemWebhookSecret: '',
  CreemTestMode: false,
  CreemProducts: '[]',
  XunhuEnabled: false,
  XunhuAppID: '',
  XunhuSecret: '',
  XunhuGateway: '',
  XunhuMinTopUp: 1,
  WaffoEnabled: false,
  WaffoApiKey: '',
  WaffoPrivateKey: '',
  WaffoPublicCert: '',
  WaffoSandboxPublicCert: '',
  WaffoSandboxApiKey: '',
  WaffoSandboxPrivateKey: '',
  WaffoSandbox: false,
  WaffoMerchantId: '',
  WaffoCurrency: 'USD',
  WaffoUnitPrice: 1,
  WaffoMinTopUp: 1,
  WaffoNotifyUrl: '',
  WaffoReturnUrl: '',
  WaffoPayMethods: '[]',
  WaffoPancakeEnabled: false,
  WaffoPancakeSandbox: false,
  WaffoPancakeMerchantID: '',
  WaffoPancakePrivateKey: '',
  WaffoPancakeWebhookPublicKey: '',
  WaffoPancakeWebhookTestKey: '',
  WaffoPancakeStoreID: '',
  WaffoPancakeProductID: '',
  WaffoPancakeReturnURL: '',
  WaffoPancakeCurrency: 'USD',
  WaffoPancakeUnitPrice: 1,
  WaffoPancakeMinTopUp: 1,
  'checkin_setting.enabled': false,
  'checkin_setting.min_quota': 1000,
  'checkin_setting.max_quota': 10000,
  'blind_box_setting.enabled': false,
  'blind_box_setting.unit_price': 2.5,
  'blind_box_setting.daily_limit': 50,
  'blind_box_setting.monthly_limit': 500,
  'blind_box_setting.daily_open_limit': 5000,
  'blind_box_setting.first_purchase_guarantee_usd': 10,
  'blind_box_setting.pity_threshold': 5,
  'blind_box_setting.pity_guarantee_usd': 10,
  'blind_box_setting.low_reward_threshold_usd': 5,
  'blind_box_setting.subscription_prize_probability': 0.003,
  'blind_box_setting.subscription_plan_title': 'Standard月卡',
  'blind_box_setting.count_options': [1, 5, 10, 20, 50],
  'blind_box_setting.tiers': [
    { name: 'starter', min_usd: 1, max_usd: 3, probability: 0.18 },
    { name: 'steady', min_usd: 4, max_usd: 7, probability: 0.3 },
    { name: 'core', min_usd: 8, max_usd: 12, probability: 0.31 },
    { name: 'boost', min_usd: 13, max_usd: 20, probability: 0.15 },
    { name: 'lucky', min_usd: 21, max_usd: 50, probability: 0.057 },
  ],
}

export function BillingSettings() {
  return (
    <SettingsPage
      routePath='/_authenticated/system-settings/billing/$section'
      defaultSettings={defaultBillingSettings}
      defaultSection={BILLING_DEFAULT_SECTION}
      getSectionContent={getBillingSectionContent}
    />
  )
}
