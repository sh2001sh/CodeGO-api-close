import { BlindBoxCard } from './components/blind-box-card'
import { WalletWorkspaceShell } from './components/wallet-workspace-shell'
import { useWalletWorkspace } from './hooks/use-wallet-workspace'

interface BlindBoxPageProps {
  initialPaymentStatus?: 'success' | 'pending' | 'fail'
}

export function BlindBoxPage(props: BlindBoxPageProps) {
  const workspace = useWalletWorkspace()

  return (
    <>
      <WalletWorkspaceShell
        title='抽奖盲盒'
        description='购买并抽取奖励，额度立即到账永久有效'
        canonicalPath='/blind-box'
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
