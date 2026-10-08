import type { ReactNode } from 'react'
import { Link } from '@tanstack/react-router'
import { ArrowUpRight } from 'lucide-react'
import { useTranslation } from '../lib/i18n'
import { CompanyIdentity, communityURL } from '../components/app/global-topbar'
import { FAQ } from '../features/public/faq'
import { PolicyArticle } from '../features/policies/policy-article'
import { companyAddress, policyDocuments, policyLanguage } from '../features/policies/documents'

const address = companyAddress

function Article(props: { title: string; children: ReactNode }) {
  const { t, locale } = useTranslation()
  return (
    <div className="site-container site-page resource-layout">
      <aside className="resource-heading">
        <h1>{t(props.title)}</h1>
        <nav aria-label={t('资源导航')}>
          <Link to="/help">{t('常见问题')}</Link>
          <Link to="/support">{t('联系支持')}</Link>
          <Link to="/about">{t('关于 CodeGo')}</Link>
          {policyDocuments.map((document) => (
            <a key={document.id} href={document.href} lang={policyLanguage(locale)}>
              {document.title[policyLanguage(locale)]}
            </a>
          ))}
        </nav>
      </aside>
      <article className="prose resource-prose">{props.children}</article>
    </div>
  )
}

export function HelpPage() {
  const { t } = useTranslation()
  return (
    <Article title="常见问题">
      <p className="resource-intro">{t('从模型选择到第一笔账单。')}</p>
      <FAQ />
      <div className="resource-next">
        <p>{t('还没有找到答案？')}</p>
        <Link to="/support" className="button button-secondary">
          {t('联系支持')}
        </Link>
      </div>
    </Article>
  )
}

export function SupportPage() {
  const { t } = useTranslation()
  return (
    <Article title="联系支持">
      <p className="resource-intro">{t('在 CodeGo 社区获取接入、账户和账单帮助。')}</p>
      <a
        className="button button-primary"
        href={communityURL}
        target="_blank"
        rel="noopener noreferrer"
      >
        {t('前往 CodeGo 社区')}
        <ArrowUpRight size={16} aria-hidden />
      </a>
      <h2>{t('提交问题前')}</h2>
      <p>
        {t(
          '请准备发生时间、模型、分组、请求编号和错误信息；支付问题请补充订单编号。服务异常可先查看服务状态。',
        )}
      </p>
      <p>
        {t(
          '公开帖子只提供脱敏信息。账户资料、付款凭证和其他个人信息请通过社区私信联系管理员；不要发送 API Key、密码或验证码。',
        )}
      </p>
      <div className="resource-actions">
        <Link to="/status">{t('服务状态')}</Link>
        <Link to="/help">{t('常见问题')}</Link>
        <Link to="/docs">{t('接入文档')}</Link>
      </div>
      <h2 id="suppliers">{t('渠道合作')}</h2>
      <p>
        {t(
          '提供上游模型服务的渠道主可在控制台提交渠道，填写模型、价格及访问条件，并完成验证与审核。渠道管理、收益和风控记录统一位于渠道工作台。',
        )}
      </p>
      <Link to="/my-channels" className="home-text-link">
        {t('进入渠道工作台')}
      </Link>
      <h2>{t('账单与发票')}</h2>
      <p>{t('已支付订单的商业发票可在账单明细自助开具与下载。退款资格、费用和进度在钱包查看。')}</p>
      <div className="resource-actions">
        <Link to="/billing" hash="invoices">
          {t('账单明细')}
        </Link>
        <Link to="/refund-policy">{t('退款规则')}</Link>
      </div>
    </Article>
  )
}

export function AboutPage() {
  const { t } = useTranslation()
  return (
    <Article title="关于 CodeGo">
      <p className="resource-intro">{t('连接模型、开发者与服务提供者。')}</p>
      <p>
        {t(
          'CodeGo AI 提供统一的模型 API 接入与渠道市场。开发者可以发现模型、比较分组、创建密钥，并在控制台核对用量和账单。',
        )}
      </p>
      <h2>{t('看得见的选择')}</h2>
      <p>
        {t(
          '公开分组展示倍率、声明模型、验证结果及最近探测信息。价格与服务状态是选择依据，实际能力以所选模型和分组为准。',
        )}
      </p>
      <div className="resource-actions">
        <Link to="/models">{t('模型与价格')}</Link>
        <Link to="/channel-market">{t('渠道市场')}</Link>
      </div>
      <h2>{t('公司信息')}</h2>
      <CompanyIdentity />
      <p>{t('CodeGo AI 由香港公司 CodeGo AI Limited（码高智能有限公司）运营。')}</p>
      <address className="company-address">{address}</address>
      <h2>{t('联系与合作')}</h2>
      <p>{t('账户支持、产品反馈与渠道合作目前通过 CodeGo 社区联系。')}</p>
      <Link to="/support" className="home-text-link">
        {t('联系支持')}
      </Link>
    </Article>
  )
}

export function PrivacyPage() {
  return <PolicyArticle documentID="privacy" />
}

export function TermsPage() {
  return <PolicyArticle documentID="terms" />
}

export function RefundPolicyPage() {
  return <PolicyArticle documentID="refund" />
}

export function SupplierPolicyPage() {
  return <PolicyArticle documentID="supplier" />
}

export function MarketRulesPage() {
  return <PolicyArticle documentID="market" />
}
