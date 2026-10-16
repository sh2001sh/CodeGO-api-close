import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { OrderInvoices } from '@/features/invoices'
import {
  isActiveUserSubscription,
  isCreditPackSubscription,
} from '@/features/subscriptions/lib'
import { CreditPackPlans } from './components/credit-pack-plans'
import { BillingHistoryDialog } from './components/dialogs/billing-history-dialog'
import { CreemConfirmDialog } from './components/dialogs/creem-confirm-dialog'
import { NowPaymentsPaymentDialog } from './components/dialogs/nowpayments-payment-dialog'
import { PaymentConfirmDialog } from './components/dialogs/payment-confirm-dialog'
import { RechargeFormCard } from './components/recharge-form-card'
import { SubscriptionClaudeConversionCard } from './components/subscription-claude-conversion-card'
import { WalletAccountOverview } from './components/wallet-account-overview'
import { WalletPagePanels } from './components/wallet-page-panels'
import { WalletTransferHistorySheet } from './components/wallet-transfer-history-sheet'
import { WalletWorkspaceShell } from './components/wallet-workspace-shell'
import { useWalletWorkspace } from './hooks/use-wallet-workspace'

interface WalletProps {
  initialShowHistory?: boolean
  initialSection?: 'funding' | 'transfer' | 'billing' | 'invoices'
  onSectionChange: (
    section: 'funding' | 'transfer' | 'billing' | 'invoices'
  ) => void
}

export function Wallet(props: WalletProps) {
  const { t } = useTranslation()
  const workspace = useWalletWorkspace()
  const setBillingDialogOpen = workspace.setBillingDialogOpen
  const activeSection = props.initialSection ?? 'funding'
  const setActiveSection = props.onSectionChange
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
        canonicalPath='/wallet'
        framedMain={false}
        main={
          <div className='commerce-wallet-content'>
            {workspace.userError ||
            workspace.subscriptionError ||
            workspace.publicPlansError ||
            workspace.topupError ? (
              <div role='alert' className='commerce-error'>
                <span>{t('加载失败')}</span>
                <Button
                  variant='outline'
                  onClick={() =>
                    void Promise.all([
                      workspace.fetchUser(),
                      workspace.fetchSubscriptionData(),
                      workspace.fetchPublicPlans(),
                      workspace.fetchTopupInfo(),
                    ])
                  }
                >
                  {t('重试')}
                </Button>
              </div>
            ) : null}
            <WalletAccountOverview
              user={workspace.userError ? null : workspace.user}
              activeSubscriptionCount={
                workspace.subscriptionError
                  ? undefined
                  : workspace.subscriptionData?.subscriptions?.length
              }
              creditPackBalance={
                workspace.subscriptionError
                  ? undefined
                  : workspace.subscriptionData?.subscriptions
                      .filter(
                        (record) =>
                          isCreditPackSubscription(record.subscription) &&
                          isActiveUserSubscription(record.subscription)
                      )
                      .reduce(
                        (total, record) =>
                          total +
                          Math.max(
                            0,
                            record.subscription.amount_total -
                              record.subscription.amount_used
                          ),
                        0
                      )
              }
              onOpenBillingHistory={() => workspace.setBillingDialogOpen(true)}
              onOpenTransferHistory={() => setTransferHistoryOpen(true)}
            />

            <Tabs
              className='commerce-wallet-sections'
              value={activeSection}
              onValueChange={(value) =>
                setActiveSection(
                  value as 'funding' | 'transfer' | 'billing' | 'invoices'
                )
              }
            >
              <TabsList variant='line' className='commerce-wallet-nav'>
                <TabsTrigger value='funding'>
                  {t('Top up and redeem')}
                </TabsTrigger>
                <TabsTrigger value='invoices'>
                  {t('invoice.commercial')}
                </TabsTrigger>
                <TabsTrigger value='transfer'>
                  {t('Quota transfer')}
                </TabsTrigger>
                <TabsTrigger value='billing'>
                  {t('Billing settings')}
                </TabsTrigger>
              </TabsList>

              <TabsContent value='funding'>
                <div className='commerce-wallet-funding'>
                  <CreditPackPlans
                    plans={
                      workspace.publicPlansError ? [] : workspace.publicPlans
                    }
                    loading={workspace.publicPlansLoading}
                    topupInfo={workspace.topupInfo}
                    onRefresh={async () => {
                      await Promise.all([
                        workspace.fetchUser(),
                        workspace.fetchSubscriptionData(),
                      ])
                    }}
                  />
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
                    selectedPaymentMethod={workspace.selectedPaymentMethod}
                    paymentLoading={workspace.paymentLoading}
                    redemptionCode={workspace.redemptionCode}
                    onRedemptionCodeChange={workspace.setRedemptionCode}
                    onRedeem={workspace.handleRedeem}
                    redeeming={workspace.redeeming}
                    topupLink={workspace.topupInfo?.topup_link}
                    loading={workspace.topupLoading}
                    showRedemptionSection={true}
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
                  />

                  {workspace.subscriptionData?.subscriptions?.some(
                    (item) =>
                      !isCreditPackSubscription(item.subscription) &&
                      item.subscription.conversion_preview?.eligible === true
                  ) && (
                    <div className='app-page-shell p-4'>
                      <SubscriptionClaudeConversionCard
                        subscriptionData={workspace.subscriptionData}
                        loading={workspace.subscriptionLoading}
                        mode='wallet'
                        onRefresh={async () => {
                          await Promise.all([
                            workspace.fetchSubscriptionData(),
                            workspace.fetchUser(),
                          ])
                        }}
                      />
                    </div>
                  )}
                </div>
              </TabsContent>

              <TabsContent value='invoices'>
                <OrderInvoices />
              </TabsContent>

              {(['transfer', 'billing'] as const).map((section) => (
                <TabsContent key={section} value={section}>
                  <WalletPagePanels
                    plans={
                      workspace.publicPlansError ? [] : workspace.publicPlans
                    }
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
                    section={section}
                    onOpenTransferHistory={() => setTransferHistoryOpen(true)}
                  />
                </TabsContent>
              ))}
            </Tabs>
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
      />

      {workspace.nowPaymentsPayment ? (
        <NowPaymentsPaymentDialog
          payment={workspace.nowPaymentsPayment}
          onClose={workspace.clearNowPaymentsPayment}
        />
      ) : null}

      <BillingHistoryDialog
        open={workspace.billingDialogOpen}
        onOpenChange={workspace.setBillingDialogOpen}
        onOpenInvoices={() => {
          workspace.setBillingDialogOpen(false)
          setActiveSection('invoices')
        }}
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
