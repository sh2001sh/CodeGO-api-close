import { Link } from '@tanstack/react-router'
import { useTranslation } from '../../lib/i18n'
import {
  companyAddress,
  getPolicyDocument,
  policyContentVersion,
  policyDocuments,
  policyInterface,
  policyLanguage,
  type PolicyDocumentID,
} from './documents'
import './policies.css'

export function PolicyArticle({ documentID }: { documentID: PolicyDocumentID }) {
  const { locale } = useTranslation()
  const language = policyLanguage(locale)
  const labels = policyInterface[locale]
  const document = getPolicyDocument(documentID)
  return (
    <div className="site-container site-page resource-layout policy-layout">
      <aside className="resource-heading policy-sidebar">
        <p className="policy-eyebrow">{labels.documents}</p>
        <nav aria-label={labels.documents}>
          {policyDocuments.map((item) => (
            <a
              key={item.id}
              href={item.href}
              aria-current={item.id === document.id ? 'page' : undefined}
              lang={language}
            >
              {item.title[language]}
            </a>
          ))}
        </nav>
        <details className="policy-contents" open>
          <summary>{labels.contents}</summary>
          <ol lang={language} dir="ltr">
            {document.sections.map((section) => (
              <li key={section.id}>
                <a href={`#${section.id}`}>{section.title[language]}</a>
              </li>
            ))}
          </ol>
        </details>
        <Link to="/support" className="policy-support-link">
          {labels.support}
        </Link>
      </aside>
      <div className="policy-main">
        {labels.fallback && (
          <p className="policy-language-notice" role="note">
            {labels.fallback}
          </p>
        )}
        <article className="prose resource-prose policy-prose" lang={language} dir="ltr">
          <header className="policy-header">
            <h1>{document.title[language]}</h1>
            <p className="resource-updated">
              <span lang={locale}>{labels.version}</span>{' '}
              <time dateTime={policyContentVersion}>{policyContentVersion}</time>
            </p>
            <p className="resource-intro">{document.introduction[language]}</p>
          </header>
          {document.sections.map((section) => (
            <section key={section.id} aria-labelledby={section.id}>
              <h2 id={section.id}>{section.title[language]}</h2>
              {section.paragraphs.map((paragraph, index) => (
                <p key={index}>{paragraph[language]}</p>
              ))}
            </section>
          ))}
          {documentID === 'refund' && (
            <div className="resource-actions">
              <Link to="/wallet">
                {language === 'en'
                  ? 'Wallet and refund quotes'
                  : language === 'zh-HK'
                    ? '錢包與退款報價'
                    : '钱包与退款报价'}
              </Link>
              <Link to="/billing" hash="invoices">
                {language === 'en'
                  ? 'Billing and invoices'
                  : language === 'zh-HK'
                    ? '帳單與發票'
                    : '账单与发票'}
              </Link>
            </div>
          )}
          <footer className="policy-company">
            <p>CodeGo AI Limited</p>
            <p lang="zh-HK">碼高智能有限公司</p>
            <address className="company-address">{companyAddress}</address>
            <Link to="/support">
              <span lang={locale}>{labels.support}</span>
            </Link>
          </footer>
        </article>
      </div>
    </div>
  )
}
