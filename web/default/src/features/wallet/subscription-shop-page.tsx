import { SubscriptionPlansCard } from './components/subscription-plans-card'
import { WalletStatsCard } from './components/wallet-stats-card'
import { WalletWorkspaceShell } from './components/wallet-workspace-shell'
import { useWalletWorkspace } from './hooks/use-wallet-workspace'

export function SubscriptionShopPage() {
  const workspace = useWalletWorkspace()

  return (
    <WalletWorkspaceShell
      title='套餐购买'
      description='先查看当前生效中的 GPT 套餐额度，再选购新的月卡或日卡。套餐仅用于官方 GPT、Codex、Responses 分组，不能转换为通用额度。'
      framedMain={false}
      main={
        <SubscriptionPlansCard
          topupInfo={workspace.topupInfo}
          plans={workspace.publicPlans}
          plansLoading={workspace.publicPlansLoading}
          subscriptionData={workspace.subscriptionData}
          subscriptionLoading={workspace.subscriptionLoading}
          onSubscriptionRefresh={workspace.fetchSubscriptionData}
          onPlansRefresh={workspace.fetchPublicPlans}
        />
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
