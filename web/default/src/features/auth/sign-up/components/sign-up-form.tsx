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
import { useEffect, useMemo, useState } from 'react'
import type { z } from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import i18n from '@/i18n/config'
import { ArrowRight, Loader2, Lock, Mail, Tag, User } from 'lucide-react'
import { Trans, useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { useStatus } from '@/hooks/use-status'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { PasswordInput } from '@/components/password-input'
import { Turnstile } from '@/components/turnstile'
import { register, wechatLoginByCode } from '@/features/auth/api'
import { LegalConsent } from '@/features/auth/components/legal-consent'
import { OAuthProviders } from '@/features/auth/components/oauth-providers'
import { registerFormSchema } from '@/features/auth/constants'
import { useAuthRedirect } from '@/features/auth/hooks/use-auth-redirect'
import { useEmailVerification } from '@/features/auth/hooks/use-email-verification'
import { useTurnstile } from '@/features/auth/hooks/use-turnstile'
import {
  getAffiliateCode,
  removeAffiliateCode,
  saveAffiliateCode,
} from '@/features/auth/lib/storage'

export function SignUpForm({
  redirectTo,
  className,
  ...props
}: React.HTMLAttributes<HTMLFormElement> & { redirectTo?: string }) {
  const { t } = useTranslation()
  const [isLoading, setIsLoading] = useState(false)
  const [verificationCode, setVerificationCode] = useState('')
  const [agreedToLegal, setAgreedToLegal] = useState(false)
  const [wechatCode, setWeChatCode] = useState('')
  const [isWeChatDialogOpen, setIsWeChatDialogOpen] = useState(false)
  const [isWeChatSubmitting, setIsWeChatSubmitting] = useState(false)
  const legalConsentErrorMessage = t('Please agree to the legal terms first')
  const initialAffiliateCode = useMemo(() => {
    if (typeof window === 'undefined') return ''
    return (
      new URLSearchParams(window.location.search).get('aff')?.trim() ||
      getAffiliateCode()
    )
  }, [])

  const { status } = useStatus()
  const {
    isTurnstileEnabled,
    turnstileSiteKey,
    turnstileToken,
    setTurnstileToken,
    validateTurnstile,
  } = useTurnstile()
  const { handleLoginSuccess } = useAuthRedirect()
  const {
    isSending: isSendingCode,
    secondsLeft,
    isActive,
    sendCode,
  } = useEmailVerification({
    turnstileToken,
    validateTurnstile,
  })

  const form = useForm<z.infer<typeof registerFormSchema>>({
    resolver: zodResolver(registerFormSchema),
    defaultValues: {
      username: '',
      email: '',
      aff: initialAffiliateCode,
      password: '',
      confirmPassword: '',
    },
  })

  const emailValue = form.watch('email')
  const affiliateCode = form.watch('aff')
  const emailVerificationRequired = !!status?.email_verification
  const hasUserAgreement = Boolean(status?.user_agreement_enabled)
  const hasPrivacyPolicy = Boolean(status?.privacy_policy_enabled)
  const requiresLegalConsent = hasUserAgreement || hasPrivacyPolicy
  const oauthRegisterEnabled =
    status?.oauth_register_enabled ??
    status?.data?.oauth_register_enabled ??
    true
  const hasWeChatLogin = Boolean(status?.wechat_login)

  const wechatQrCodeUrl = useMemo(() => {
    return (
      status?.wechat_qrcode ||
      status?.wechat_qr_code ||
      status?.wechat_qrcode_image_url ||
      status?.wechat_qr_code_image_url ||
      status?.wechat_account_qrcode_image_url ||
      status?.WeChatAccountQRCodeImageURL ||
      status?.data?.wechat_qrcode ||
      status?.data?.WeChatAccountQRCodeImageURL ||
      ''
    )
  }, [status])

  useEffect(() => {
    if (requiresLegalConsent) {
      setAgreedToLegal(false)
    } else {
      setAgreedToLegal(true)
    }
  }, [requiresLegalConsent])

  useEffect(() => {
    const normalizedAffiliateCode = affiliateCode.trim()
    if (normalizedAffiliateCode) {
      saveAffiliateCode(normalizedAffiliateCode)
      return
    }
    removeAffiliateCode()
  }, [affiliateCode])

  async function onSubmit(data: z.infer<typeof registerFormSchema>) {
    if (requiresLegalConsent && !agreedToLegal) {
      toast.error(legalConsentErrorMessage)
      return
    }

    if (emailVerificationRequired) {
      if (!data.email) {
        toast.error(t('Please enter your email'))
        return
      }
      if (!verificationCode) {
        toast.error(t('Please enter the verification code'))
        return
      }
    }

    setIsLoading(true)
    try {
      const res = await register({
        username: data.username,
        password: data.password,
        email: data.email || undefined,
        verification_code: verificationCode || undefined,
        aff_code: data.aff.trim() || undefined,
        turnstile: turnstileToken,
      })

      if (res?.success) {
        removeAffiliateCode()
        await handleLoginSuccess(res.data as { id?: number } | null, redirectTo)
        toast.success(t('Account created!'))
      }
    } catch (_error) {
      // Errors are handled by global interceptor
    } finally {
      setIsLoading(false)
    }
  }

  async function handleSendVerificationCode() {
    await sendCode(emailValue || '')
  }

  const handleOpenWeChatDialog = () => {
    if (requiresLegalConsent && !agreedToLegal) {
      toast.error(legalConsentErrorMessage)
      return
    }

    setIsWeChatDialogOpen(true)
  }

  const handleWeChatDialogChange = (open: boolean) => {
    setIsWeChatDialogOpen(open)
    if (!open) {
      setWeChatCode('')
      setIsWeChatSubmitting(false)
    }
  }

  async function handleWeChatLogin() {
    if (!wechatCode.trim()) {
      toast.error(t('Please enter the verification code'))
      return
    }

    setIsWeChatSubmitting(true)
    try {
      const res = await wechatLoginByCode(wechatCode)
      if (res?.success) {
        await handleLoginSuccess(res.data as { id?: number } | null)
        toast.success(t('Signed in via WeChat'))
        handleWeChatDialogChange(false)
      } else {
        toast.error(res?.message || t('Login failed'))
      }
    } catch (_error) {
      toast.error(t('Login failed'))
    } finally {
      setIsWeChatSubmitting(false)
    }
  }

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit(onSubmit)}
        aria-busy={isLoading}
        className={cn('grid gap-4', className)}
        {...props}
      >
        {/* Username */}
        <FormField
          control={form.control}
          name='username'
          render={({ field }) => (
            <FormItem>
              <FormLabel className='text-foreground/80 text-xs font-semibold'>
                {t('Username')}
              </FormLabel>
              <FormControl>
                <div className='relative'>
                  <User className='text-muted-foreground/60 pointer-events-none absolute top-1/2 left-3.5 size-4 -translate-y-1/2' />
                  <Input
                    placeholder={t('Enter your username')}
                    autoComplete='username'
                    className='border-border/80 focus:border-foreground/50 h-12 rounded-2xl bg-white pl-10 text-[15px] transition-all dark:bg-white/[0.05]'
                    {...field}
                  />
                </div>
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        {/* Email Field (when required) */}
        {emailVerificationRequired && (
          <>
            <FormField
              control={form.control}
              name='email'
              render={({ field }) => (
                <FormItem>
                  <FormLabel className='text-foreground/80 text-xs font-semibold'>
                    {t('Email (required for verification)')}
                  </FormLabel>
                  <FormControl>
                    <div className='relative'>
                      <Mail className='text-muted-foreground/60 pointer-events-none absolute top-1/2 left-3.5 size-4 -translate-y-1/2' />
                      <Input
                        placeholder='name@example.com'
                        type='email'
                        autoComplete='email'
                        className='border-border/80 focus:border-foreground/50 h-12 rounded-2xl bg-white pl-10 text-[15px] transition-all dark:bg-white/[0.05]'
                        {...field}
                      />
                    </div>
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <div className='flex items-end gap-2'>
              <div className='flex-1'>
                <label
                  htmlFor='registration-verification-code'
                  className='text-foreground/80 mb-1.5 block text-xs font-semibold'
                >
                  {t('Verification code')}
                </label>
                <Input
                  id='registration-verification-code'
                  autoComplete='one-time-code'
                  placeholder={t('Verification code')}
                  value={verificationCode}
                  onChange={(e) => setVerificationCode(e.target.value)}
                  className='border-border/80 focus:border-foreground/50 h-12 rounded-2xl bg-white text-[15px] dark:bg-white/[0.05]'
                />
              </div>
              <Button
                variant='outline'
                type='button'
                disabled={isLoading || isSendingCode || isActive || !emailValue}
                onClick={handleSendVerificationCode}
                className='border-border/80 h-12 rounded-2xl px-4 font-medium'
              >
                {isActive ? (
                  t('Resend ({{seconds}}s)', { seconds: secondsLeft })
                ) : isSendingCode ? (
                  <Loader2 className='h-4 w-4 animate-spin' />
                ) : (
                  t('Send code')
                )}
              </Button>
            </div>

            {isTurnstileEnabled && (
              <div className='mt-2'>
                <Turnstile
                  siteKey={turnstileSiteKey}
                  onVerify={setTurnstileToken}
                />
              </div>
            )}
          </>
        )}

        {/* Password */}
        <FormField
          control={form.control}
          name='password'
          render={({ field }) => (
            <FormItem>
              <FormLabel className='text-foreground/80 text-xs font-semibold'>
                {t('Password')}
              </FormLabel>
              <FormControl>
                <div className='relative'>
                  <Lock className='text-muted-foreground/60 pointer-events-none absolute top-1/2 left-3.5 size-4 -translate-y-1/2' />
                  <PasswordInput
                    placeholder={t('Enter password (8-20 characters)')}
                    autoComplete='new-password'
                    className='[&_input]:border-border/80 [&_input]:focus:border-foreground/50 [&_input]:h-12 [&_input]:rounded-2xl [&_input]:bg-white [&_input]:pl-10 [&_input]:text-[15px] dark:[&_input]:bg-white/[0.05]'
                    {...field}
                  />
                </div>
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        {/* Confirm Password */}
        <FormField
          control={form.control}
          name='confirmPassword'
          render={({ field }) => (
            <FormItem>
              <FormLabel className='text-foreground/80 text-xs font-semibold'>
                {t('Confirm password')}
              </FormLabel>
              <FormControl>
                <div className='relative'>
                  <Lock className='text-muted-foreground/60 pointer-events-none absolute top-1/2 left-3.5 size-4 -translate-y-1/2' />
                  <PasswordInput
                    placeholder={t('Confirm password')}
                    autoComplete='new-password'
                    className='[&_input]:border-border/80 [&_input]:focus:border-foreground/50 [&_input]:h-12 [&_input]:rounded-2xl [&_input]:bg-white [&_input]:pl-10 [&_input]:text-[15px] dark:[&_input]:bg-white/[0.05]'
                    {...field}
                  />
                </div>
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        {/* Invitation Code (Optional) */}
        <FormField
          control={form.control}
          name='aff'
          render={({ field }) => (
            <FormItem>
              <FormLabel className='text-foreground/80 text-xs font-semibold'>
                <Trans i18nKey={'邀请码（可选）'} />
              </FormLabel>
              <FormControl>
                <div className='relative'>
                  <Tag className='text-muted-foreground/60 pointer-events-none absolute top-1/2 left-3.5 size-4 -translate-y-1/2' />
                  <Input
                    autoComplete='off'
                    maxLength={32}
                    placeholder={i18n.t('输入邀请码（选填）')}
                    className='border-border/80 focus:border-foreground/50 h-12 rounded-2xl bg-white pl-10 text-[15px] transition-all dark:bg-white/[0.05]'
                    {...field}
                  />
                </div>
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        {/* Legal Consent */}
        <LegalConsent
          status={status}
          checked={agreedToLegal}
          onCheckedChange={setAgreedToLegal}
          className='mt-1 rounded-2xl'
        />

        {/* Primary Action Button (OpenDesign Obsidian Pill) */}
        <Button
          type='submit'
          className='market-auth-submit-btn mt-2.5 flex w-full items-center justify-center gap-2 text-[15px]'
          disabled={isLoading || (requiresLegalConsent && !agreedToLegal)}
        >
          {isLoading ? (
            <Loader2 className='size-4 animate-spin' />
          ) : (
            <ArrowRight className='size-4 transition-transform group-hover:translate-x-0.5' />
          )}
          {t('Create account with email')}
        </Button>

        {oauthRegisterEnabled && (
          <OAuthProviders
            status={status}
            redirectTo={redirectTo}
            disabled={isLoading || (requiresLegalConsent && !agreedToLegal)}
            onWeChatLogin={hasWeChatLogin ? handleOpenWeChatDialog : undefined}
            isWeChatLoading={isWeChatSubmitting}
            className='pt-2'
          />
        )}
      </form>

      {hasWeChatLogin && (
        <Dialog
          open={isWeChatDialogOpen}
          onOpenChange={handleWeChatDialogChange}
        >
          <DialogContent className='max-w-sm'>
            <DialogHeader className='text-left'>
              <DialogTitle>{t('WeChat sign in')}</DialogTitle>
              <DialogDescription>
                <Trans
                  i18nKey={'扫描二维码关注公众号，回复“验证码”获取登录验证码。'}
                />
              </DialogDescription>
            </DialogHeader>

            {wechatQrCodeUrl ? (
              <div className='flex justify-center'>
                <img
                  src={wechatQrCodeUrl}
                  alt={t('WeChat login QR code')}
                  className='h-40 w-40 rounded-md border object-contain'
                />
              </div>
            ) : (
              <p className='text-muted-foreground text-sm'>
                {t('QR code is not configured. Please contact support.')}
              </p>
            )}

            <div className='grid gap-2'>
              <Label htmlFor='wechat-code'>{t('Verification code')}</Label>
              <Input
                id='wechat-code'
                placeholder={t('Enter the verification code')}
                value={wechatCode}
                onChange={(event) => setWeChatCode(event.target.value)}
                autoComplete='one-time-code'
              />
            </div>

            <DialogFooter>
              <Button
                type='button'
                variant='outline'
                onClick={() => handleWeChatDialogChange(false)}
                disabled={isWeChatSubmitting}
              >
                {t('Cancel')}
              </Button>
              <Button
                type='button'
                onClick={handleWeChatLogin}
                disabled={
                  isWeChatSubmitting ||
                  !wechatCode.trim() ||
                  (requiresLegalConsent && !agreedToLegal)
                }
                className='gap-2'
              >
                {isWeChatSubmitting ? (
                  <Loader2 className='h-4 w-4 animate-spin' />
                ) : null}
                {t('Confirm')}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </Form>
  )
}
