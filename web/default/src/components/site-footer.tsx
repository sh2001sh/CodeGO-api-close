/* Copyright (C) 2023-2026 QuantumNous. AGPL-3.0-or-later. */
import { Link } from '@tanstack/react-router'
import { ArrowUpRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { normalizeSystemName } from '@/lib/branding'
import { COMMUNITY_LINKS, COMPANY } from '@/lib/company'
import './site-footer.css'

export function SiteFooter() {
  const { t } = useTranslation()

  return (
    <footer className='site-footer' aria-label={t('siteFooter.navigation')}>
      <div className='site-footer-inner'>
        <div className='site-footer-directory'>
          <Link className='site-footer-brand' to='/'>
            <img src='/code-go-logo.svg' width='36' height='36' alt='' />
            <span>{normalizeSystemName()}</span>
          </Link>

          <nav
            className='site-footer-group'
            aria-label={t('siteFooter.product')}
          >
            <h2>{t('siteFooter.product')}</h2>
            <Link to='/pricing'>{t('siteFooter.models')}</Link>
            <Link to='/market'>{t('siteFooter.marketplace')}</Link>
            <Link
              to='/rankings'
              search={{
                sort: 'score',
                page: 1,
                hours: 24,
                metric: 'request_count',
              }}
            >
              {t('siteFooter.rankings')}
            </Link>
            <Link to='/packages'>{t('siteFooter.packages')}</Link>
            <Link to='/dashboard/$section' params={{ section: 'overview' }}>
              {t('siteFooter.console')}
            </Link>
          </nav>

          <nav
            className='site-footer-group'
            aria-label={t('siteFooter.developers')}
          >
            <h2>{t('siteFooter.developers')}</h2>
            <Link to='/docs'>{t('siteFooter.documentation')}</Link>
            <Link to='/docs' search={{ article: 'quickstart' }}>
              {t('siteFooter.quickstart')}
            </Link>
            <Link to='/docs' search={{ article: 'billing' }}>
              {t('siteFooter.billing')}
            </Link>
            <Link to='/keys'>{t('siteFooter.apiKeys')}</Link>
            <Link to='/playground'>{t('siteFooter.playground')}</Link>
            <Link to='/group-status'>{t('siteFooter.status')}</Link>
          </nav>

          <nav
            className='site-footer-group'
            aria-label={t('siteFooter.community')}
          >
            <h2>{t('siteFooter.community')}</h2>
            {COMMUNITY_LINKS.map((link) => (
              <a
                key={link.href}
                href={link.href}
                target='_blank'
                rel='noopener noreferrer'
              >
                {link.label === 'Community'
                  ? t('siteFooter.communityForum')
                  : link.label}
                <ArrowUpRight aria-hidden='true' />
              </a>
            ))}
          </nav>

          <nav
            className='site-footer-group'
            aria-label={t('siteFooter.company')}
          >
            <h2>{t('siteFooter.company')}</h2>
            <Link to='/about'>{t('siteFooter.about')}</Link>
            <Link to='/privacy-policy'>{t('Privacy Policy')}</Link>
            <Link to='/user-agreement'>{t('User Agreement')}</Link>
          </nav>
        </div>

        <div className='site-footer-legal'>
          <span>
            © {new Date().getFullYear()} {COMPANY.name}
          </span>
          <Link to='/about'>
            {t('Business registration number')} {COMPANY.registrationNumber}
          </Link>
        </div>
      </div>
    </footer>
  )
}
