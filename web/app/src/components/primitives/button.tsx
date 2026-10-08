import { Button as BaseButton } from '@base-ui/react/button'
import type { ComponentProps } from 'react'
import { useTranslation } from '../../lib/i18n'

export type ButtonVariant = 'primary' | 'secondary' | 'quiet' | 'ghost' | 'danger' | 'danger-solid'
export type ButtonSize = 'sm' | 'md' | 'lg'

/**
 * Primary action control. `quiet` is kept as an alias of `secondary` for older call sites.
 * `loading` disables the button and shows an inline spinner without changing its width.
 */
export function Button(
  props: ComponentProps<typeof BaseButton> & {
    variant?: ButtonVariant
    size?: ButtonSize
    loading?: boolean
    iconOnly?: boolean
  },
) {
  const {
    variant = 'primary',
    size = 'md',
    loading,
    iconOnly,
    disabled,
    className = '',
    ...rest
  } = props
  return (
    <BaseButton
      {...rest}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      data-loading={loading || undefined}
      data-size={size === 'md' ? undefined : size}
      data-icon-only={iconOnly || undefined}
      className={`button button-${variant} ${className}`.trim()}
    />
  )
}

/** Square icon button; `label` is required so the control always has an accessible name. */
export function IconButton(
  props: Omit<ComponentProps<'button'>, 'aria-label'> & { label: string; bordered?: boolean },
) {
  const { t } = useTranslation()
  const { label, bordered, className = '', type = 'button', ...rest } = props
  return (
    <button
      {...rest}
      type={type}
      aria-label={t(label)}
      title={t(label)}
      data-bordered={bordered || undefined}
      className={`icon-button ${className}`.trim()}
    />
  )
}
