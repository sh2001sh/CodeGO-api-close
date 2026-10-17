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
import { Link } from '@tanstack/react-router'
import { ArrowUpRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { COMMUNITY_LINKS, COMPANY } from '@/lib/company'
import { DEFAULT_LOGO } from '@/lib/constants'
import { getPublicPageSeoEntry } from '@/lib/public-page-seo'
import { PublicLayout } from '@/components/layout'
import { SiteSeo } from '@/components/seo'
import '@/features/docs/reading.css'
import './about.css'

const aboutSeo = getPublicPageSeoEntry('/about')

export function About() {
  const { t } = useTranslation()
  return (
    <PublicLayout>
      <SiteSeo
        title={aboutSeo.title}
        description={aboutSeo.description}
        keywords={aboutSeo.keywords}
        canonicalPath={aboutSeo.path}
      />
      <article className='market-reading market-reading-width codego-about'>
        <header className='codego-about-heading'>
          <h1>{t(aboutSeo.h1)}</h1>
          <img src={DEFAULT_LOGO} width='80' height='80' alt='' />
        </header>
        <div className='codego-about-product'>
          {aboutSeo.sections.map((section) => (
            <section key={section.heading}>
              <h2>{t(section.heading)}</h2>
              {section.paragraphs.map((paragraph) => (
                <p key={paragraph}>{t(paragraph)}</p>
              ))}
            </section>
          ))}
          <nav className='codego-about-links' aria-label={t('产品')}>
            <Link to='/pricing'>
              {t('模型')}
              <ArrowUpRight aria-hidden />
            </Link>
            <Link to='/market'>
              {t('市场')}
              <ArrowUpRight aria-hidden />
            </Link>
            <Link to='/docs'>
              {t('文档')}
              <ArrowUpRight aria-hidden />
            </Link>
          </nav>
        </div>
        <section
          className='codego-about-company'
          aria-labelledby='company-title'
        >
          <h2 id='company-title'>{COMPANY.name}</h2>
          <dl>
            <div>
              <dt>{t('公司名称')}</dt>
              <dd>{t('码高智能有限公司')}</dd>
            </div>
            <div>
              <dt>{t('Business registration number')}</dt>
              <dd className='tabular-nums'>{COMPANY.registrationNumber}</dd>
            </div>
            <div>
              <dt>{t('Registered address')}</dt>
              <dd>
                <address>{COMPANY.address}</address>
              </dd>
            </div>
          </dl>
          <nav className='codego-about-links' aria-label={t('Community')}>
            {COMMUNITY_LINKS.map((link) => (
              <a
                key={link.href}
                href={link.href}
                target='_blank'
                rel='noopener noreferrer'
              >
                {link.label === 'Community' ? t('Community') : link.label}
                <ArrowUpRight aria-hidden />
              </a>
            ))}
          </nav>
        </section>
      </article>
    </PublicLayout>
  )
}
