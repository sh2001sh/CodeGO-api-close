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
        description='每盒附赠一个当日幸运号，奖励即时到账'
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
