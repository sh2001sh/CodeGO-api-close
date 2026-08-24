import { useEffect, useState } from 'react'
import { CreditCard, Send, SlidersHorizontal } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { BillingHistoryDialog } from './components/dialogs/billing-history-dialog'
import { CreemConfirmDialog } from './components/dialogs/creem-confirm-dialog'
import { PaymentConfirmDialog } from './components/dialogs/payment-confirm-dialog'
import { RechargeFormCard } from './components/recharge-form-card'
import { WalletAccountOverview } from './components/wallet-account-overview'
import { WalletPagePanels } from './components/wallet-page-panels'
import { WalletTransferHistorySheet } from './components/wallet-transfer-history-sheet'
import { WalletWorkspaceShell } from './components/wallet-workspace-shell'
import { useWalletWorkspace } from './hooks/use-wallet-workspace'

interface WalletProps {
  initialShowHistory?: boolean
}

export function Wallet(props: WalletProps) {
  const { t } = useTranslation()
  const workspace = useWalletWorkspace()
  const setBillingDialogOpen = workspace.setBillingDialogOpen
  const [activeSection, setActiveSection] = useState<
    'funding' | 'transfer' | 'billing'
  >('funding')
  const [transferHistoryOpen, setTransferHistoryOpen] = useState(false)

  useEffect(() => {
    if (!props.initialShowHistory) return
    setBillingDialogOpen(true)
    if (typeof window !== 'undefined') {
      window.history.replaceState({}, '', window.location.pathname)
    }
  }, [props.initialShowHistory, setBillingDialogOpen])

  return (
    <>
      <WalletWorkspaceShell
        title={t('Wallet')}
        description={t('在一个页面管理统一额度充值、安全转账和扣费优先级。')}
        canonicalPath='/wallet'
        framedMain={false}
        main={
          <div className='flex min-w-0 flex-col gap-4'>
            <WalletAccountOverview
              user={workspace.user}
              activeSubscriptionCount={
                workspace.subscriptionData?.subscriptions?.length ?? 0
              }
              onSelectFunding={() => setActiveSection('funding')}
              onSelectTransfer={() => setActiveSection('transfer')}
              onOpenBillingHistory={() => workspace.setBillingDialogOpen(true)}
              onOpenTransferHistory={() => setTransferHistoryOpen(true)}
            />

            <Tabs
              value={activeSection}
              onValueChange={(value) =>
                setActiveSection(value as 'funding' | 'transfer' | 'billing')
              }
            >
              <TabsList className='codego-wallet-tabs grid h-auto w-full grid-cols-3'>
                <TabsTrigger value='funding'>
                  <CreditCard className='size-4' />
                  {t('Top up and redeem')}
                </TabsTrigger>
                <TabsTrigger value='transfer'>
                  <Send className='size-4' />
                  {t('Quota transfer')}
                </TabsTrigger>
                <TabsTrigger value='billing'>
                  <SlidersHorizontal className='size-4' />
                  {t('Billing settings')}
                </TabsTrigger>
              </TabsList>
            </Tabs>

            {activeSection === 'funding' ? (
              <RechargeFormCard
                topupInfo={workspace.topupInfo}
                presetAmounts={workspace.presetAmounts}
                selectedPreset={workspace.selectedPreset}
                onSelectPreset={workspace.handleSelectPreset}
                topupAmount={workspace.topupAmount}
                onTopupAmountChange={workspace.handleTopupAmountChange}
                paymentAmount={workspace.paymentAmount}
                calculating={workspace.calculating}
                onPaymentMethodSelect={workspace.handlePaymentMethodSelect}
                paymentLoading={workspace.paymentLoading}
                redemptionCode={workspace.redemptionCode}
                onRedemptionCodeChange={workspace.setRedemptionCode}
                onRedeem={workspace.handleRedeem}
                redeeming={workspace.redeeming}
                topupLink={workspace.topupInfo?.topup_link}
                loading={workspace.topupLoading}
                showRedemptionSection={false}
                usdExchangeRate={workspace.effectiveUsdExchangeRate}
                creemProducts={workspace.topupInfo?.creem_products}
                enableCreemTopup={workspace.topupInfo?.enable_creem_topup}
                onCreemProductSelect={workspace.handleCreemProductSelect}
                enableWaffoTopup={workspace.topupInfo?.enable_waffo_topup}
                waffoPayMethods={workspace.topupInfo?.waffo_pay_methods}
                onWaffoMethodSelect={workspace.handleWaffoMethodSelect}
                enableWaffoPancakeTopup={
                  workspace.topupInfo?.enable_waffo_pancake_topup
                }
                compact
              />
            ) : null}

            <WalletPagePanels
              plans={workspace.publicPlans}
              plansLoading={workspace.publicPlansLoading}
              loading={workspace.userLoading}
              topupLink={workspace.topupInfo?.topup_link}
              redemptionCode={workspace.redemptionCode}
              onRedemptionCodeChange={workspace.setRedemptionCode}
              onRedeem={workspace.handleRedeem}
              redeeming={workspace.redeeming}
              subscriptionData={workspace.subscriptionData}
              subscriptionLoading={workspace.subscriptionLoading}
              onSubscriptionRefresh={workspace.fetchSubscriptionData}
              onUserRefresh={workspace.fetchUser}
              section={activeSection}
              onOpenTransferHistory={() => setTransferHistoryOpen(true)}
            />
          </div>
        }
      />

      <PaymentConfirmDialog
        open={workspace.confirmDialogOpen}
        onOpenChange={workspace.setConfirmDialogOpen}
        onConfirm={workspace.handlePaymentConfirm}
        topupAmount={workspace.topupAmount}
        paymentAmount={workspace.paymentAmount}
        paymentMethod={workspace.selectedPaymentMethod}
        calculating={workspace.calculating}
        processing={workspace.processing || workspace.pancakeProcessing}
        discountRate={workspace.getDiscountRate()}
        usdExchangeRate={workspace.effectiveUsdExchangeRate}
      />

      <BillingHistoryDialog
        open={workspace.billingDialogOpen}
        onOpenChange={workspace.setBillingDialogOpen}
      />

      <WalletTransferHistorySheet
        open={transferHistoryOpen}
        onOpenChange={setTransferHistoryOpen}
      />

      <CreemConfirmDialog
        open={workspace.creemDialogOpen}
        onOpenChange={workspace.setCreemDialogOpen}
        onConfirm={workspace.handleCreemConfirm}
        product={workspace.selectedCreemProduct}
        processing={workspace.creemProcessing}
      />
    </>
  )
}
