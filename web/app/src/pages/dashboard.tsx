import { Link } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { ArrowUpRight, KeyRound, Wallet } from 'lucide-react'
import { walletOptions, keysOptions, sessionOptions } from '../lib/queries'
import { credits } from '../lib/format'
import { useTranslation } from '../lib/i18n'
import { PageHeader } from '../components/ui'
import { FundingEconomics } from '../features/funding-economics'

export default function DashboardPage() {
  const { t } = useTranslation()
  const wallet = useSuspenseQuery(walletOptions()).data
  const user = useSuspenseQuery(sessionOptions()).data
  const keys = useSuspenseQuery(keysOptions()).data
  return (
    <>
      <PageHeader title="仪表板" />
      <div className="overview-line">
        <h2>{user.display_name || user.username}</h2>
        <span>{t(user.group)}</span>
      </div>
      <dl className="balance-ledger">
        <div>
          <dt>{t('钱包余额')}</dt>
          <dd>{credits(wallet.balance_micro_credits)}</dd>
        </div>
        <div>
          <dt>{t('API Key')}</dt>
          <dd>{keys?.filter((key) => key.status === 'active').length ?? 0}</dd>
        </div>
      </dl>
      <section className="quick-links">
        <Link to="/wallet">
          <Wallet size={20} aria-hidden />
          <span>{t('充值与订阅')}</span>
          <ArrowUpRight size={18} aria-hidden />
        </Link>
        <Link to="/keys">
          <KeyRound size={20} aria-hidden />
          <span>{t('管理 API Key')}</span>
          <ArrowUpRight size={18} aria-hidden />
        </Link>
        <Link to="/usage-logs">
          <span>{t('查看使用日志')}</span>
          <ArrowUpRight size={18} aria-hidden />
        </Link>
      </section>
      <section className="endpoint-panel">
        <h2>{t('API 地址')}</h2>
        <code>{window.location.origin}/v1</code>
      </section>
      {user.role === 'root' && <FundingEconomics />}
    </>
  )
}
