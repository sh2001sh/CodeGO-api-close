import i18n from '@/i18n/config'
import { useTranslation } from 'react-i18next'
import { BlindBoxCard } from './components/blind-box-card'
import { WalletWorkspaceShell } from './components/wallet-workspace-shell'
import { useWalletWorkspace } from './hooks/use-wallet-workspace'

interface BlindBoxPageProps {
  initialPaymentStatus?: 'success' | 'pending' | 'fail'
}

export function BlindBoxPage(props: BlindBoxPageProps) {
  useTranslation()
  const workspace = useWalletWorkspace()

  return (
    <>
      <WalletWorkspaceShell
        title={i18n.t('抽奖盲盒')}
        canonicalPath='/blind-box'
        kicker='C·04 · BLIND BOX'
        main={
          <BlindBoxCard
            onSubscriptionRefresh={workspace.fetchSubscriptionData}
            onUserRefresh={workspace.fetchUser}
            paymentResult={props.initialPaymentStatus}
          />
        }
        sidebar={null}
        framedMain={false}
      />
    </>
  )
}
