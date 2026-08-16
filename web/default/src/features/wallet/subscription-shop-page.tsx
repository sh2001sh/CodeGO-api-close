import { SubscriptionSettlementHistory } from './components/subscription-settlement-history'
import { WalletStatsCard } from './components/wallet-stats-card'
import { WalletWorkspaceShell } from './components/wallet-workspace-shell'
import { useWalletWorkspace } from './hooks/use-wallet-workspace'

export function SubscriptionShopPage() {
  const workspace = useWalletWorkspace()

  return (
    <WalletWorkspaceShell
      title='历史套餐清算'
      description='查看月卡取消后的统一额度清算结果。月卡购买、续费、升级、燃料和集享入口已停止。'
      framedMain={false}
      main={
        <SubscriptionSettlementHistory />
      }
      sidebar={
        <WalletStatsCard
          user={workspace.user}
          plans={workspace.publicPlans}
          loading={workspace.userLoading}
          subscriptionData={workspace.subscriptionData}
          subscriptionLoading={workspace.subscriptionLoading}
          onSubscriptionRefresh={workspace.fetchSubscriptionData}
        />
      }
    />
  )
}
