import { useTranslation } from 'react-i18next'
import { COMMUNITY_LINKS, COMPANY } from '@/lib/company'

export function CompanyIdentity() {
  const { t } = useTranslation()
  return (
    <div className='company-identity'>
      <strong>{COMPANY.name}</strong>
      <nav className='company-links' aria-label={t('Community')}>
        {COMMUNITY_LINKS.map((link) => (
          <a
            key={link.href}
            href={link.href}
            target='_blank'
            rel='noopener noreferrer'
          >
            {link.label === 'Community' ? t('Community') : link.label}
          </a>
        ))}
        <a href='/privacy-policy'>{t('Privacy Policy')}</a>
        <a href='/user-agreement'>{t('User Agreement')}</a>
      </nav>
    </div>
  )
}
