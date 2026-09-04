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
import { useTranslation } from 'react-i18next'
import { getPublicPageSeoEntry } from '@/lib/public-page-seo'
import { getPrivacyPolicy } from './api'
import { LegalDocument } from './legal-document'

const privacySeo = getPublicPageSeoEntry('/privacy-policy')

export function PrivacyPolicy() {
  const { t } = useTranslation()
  return (
    <LegalDocument
      title={t('Privacy Policy')}
      seoTitle={privacySeo.title}
      seoDescription={privacySeo.description}
      seoKeywords={privacySeo.keywords}
      canonicalPath={privacySeo.path}
      queryKey='privacy-policy'
      fetchDocument={getPrivacyPolicy}
      emptyMessage={t(
        'The administrator has not configured a privacy policy yet.'
      )}
      fallbackContent={t(`# Code Go 隐私政策

Code Go 是一个围绕 AI Coding 工作流构建的平台。我们的目标并非收集尽可能多的数据，而是只保留支撑账号、计费、风控和服务稳定所必需的信息。

## 我们收集什么

- 账号信息：用户名、邮箱、登录方式、必要的身份验证信息。
- 调用记录：请求时间、模型、额度消耗、错误信息、设备与 IP 的基础安全信息。
- 支付与订单信息：充值、套餐、兑换码、积分、盲盒和订阅相关记录。
- 偏好与配置：语言、主题、扣费顺序、常用设置与授权状态。

## 我们为什么收集

- 用于完成账号登录、套餐购买、额度结算和 API 调用。
- 用于风控、防刷、防滥用和服务质量排查。
- 用于展示你的成长记录、成就进度和长期累计数据。

## 我们不会做什么

- 不会把你的私有请求内容用于公开展示。
- 不会把你的账号信息出售给第三方。
- 不会在没有业务必要的情况下长期保留敏感数据。

## 数据保留与安全

- 日志、账单与风控记录会在业务必要范围内保留。
- 我们会采取访问控制、传输加密和最小权限原则保护数据。
- 如果因合规、支付或安全要求需要额外保留，会按适用法律执行。

## 第三方服务

Code Go 可能会接入第三方模型提供方、支付服务、通知服务或安全验证服务。这些服务可能按其各自政策处理必要数据。

## 你的权利

你可以联系站点运营方申请查看、更新或删除你的部分账号信息；涉及账单、风控或合规要求的记录可能不能立即删除。

## 联系方式

如需处理隐私问题、账号数据请求或投诉，请通过站内公告、售后群或站点公开联系方式联系 Code Go。`)}
    />
  )
}
