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
import { useState } from 'react'
import type { z } from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { ArrowRight, Loader2, Mail } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { useCountdown } from '@/hooks/use-countdown'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Turnstile } from '@/components/turnstile'
import { sendPasswordResetEmail } from '@/features/auth/api'
import {
  forgotPasswordFormSchema,
  PASSWORD_RESET_COUNTDOWN,
} from '@/features/auth/constants'
import { useTurnstile } from '@/features/auth/hooks/use-turnstile'

export function ForgotPasswordForm({
  className,
  ...props
}: React.HTMLAttributes<HTMLFormElement>) {
  const { t } = useTranslation()
  const [isLoading, setIsLoading] = useState(false)

  const {
    isTurnstileEnabled,
    turnstileSiteKey,
    turnstileToken,
    setTurnstileToken,
    validateTurnstile,
  } = useTurnstile()
  const {
    secondsLeft,
    isActive,
    start: startCountdown,
  } = useCountdown({ initialSeconds: PASSWORD_RESET_COUNTDOWN })

  const form = useForm<z.infer<typeof forgotPasswordFormSchema>>({
    resolver: zodResolver(forgotPasswordFormSchema),
    defaultValues: { email: '' },
  })

  async function onSubmit(data: z.infer<typeof forgotPasswordFormSchema>) {
    if (!validateTurnstile()) return

    setIsLoading(true)
    try {
      const res = await sendPasswordResetEmail(data.email, turnstileToken)
      if (res?.success) {
        form.reset()
        startCountdown()
        toast.success(t('Reset email sent, please check your inbox'))
      }
    } catch (_error) {
      // Errors are handled by global interceptor
    } finally {
      setIsLoading(false)
    }
  }

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit(onSubmit)}
        aria-busy={isLoading}
        className={cn('grid gap-2', className)}
        {...props}
      >
        <FormField
          control={form.control}
          name='email'
          render={({ field }) => (
            <FormItem>
              <FormLabel className='text-foreground/80 text-xs font-semibold'>
                {t('Email')}
              </FormLabel>
              <FormControl>
                <div className='relative'>
                  <Mail className='text-muted-foreground/60 pointer-events-none absolute top-1/2 left-3.5 size-4 -translate-y-1/2' />
                  <Input
                    type='email'
                    autoComplete='email'
                    placeholder={t('name@example.com')}
                    className='border-border/80 focus:border-foreground/50 h-12 rounded-2xl bg-white pl-10 text-[15px] transition-all dark:bg-white/[0.05]'
                    {...field}
                  />
                </div>
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <Button
          type='submit'
          className='market-auth-submit-btn mt-3 flex w-full items-center justify-center gap-2 text-[15px]'
          disabled={isLoading || isActive}
        >
          {isActive
            ? t('Resend ({{seconds}}s)', { seconds: secondsLeft })
            : t('Send reset email')}
          {isLoading ? (
            <Loader2 className='size-4 animate-spin' />
          ) : (
            <ArrowRight className='size-4 transition-transform group-hover:translate-x-0.5' />
          )}
        </Button>

        {isTurnstileEnabled && (
          <div className='mt-2'>
            <Turnstile
              siteKey={turnstileSiteKey}
              onVerify={setTurnstileToken}
            />
          </div>
        )}
      </form>
    </Form>
  )
}
