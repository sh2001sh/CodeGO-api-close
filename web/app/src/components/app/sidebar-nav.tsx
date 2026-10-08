import { Link, useRouterState } from '@tanstack/react-router'
import { useTranslation } from '../../lib/i18n'
import { isActive, visibleGroups } from '../../lib/navigation'

/** Grouped console navigation. All groups stay expanded so nothing is hidden behind toggles. */
export function SidebarNav(props: { role?: string; onNavigate?: () => void }) {
  const { t } = useTranslation()
  const pathname = useRouterState({ select: (state) => state.location.pathname })
  return (
    <nav aria-label={t('主导航')}>
      {visibleGroups(props.role).map((group) => (
        <div className="nav-group" key={group.id}>
          <div className="nav-group-title" id={`nav-${group.id}`}>
            {t(group.title)}
          </div>
          <ul aria-labelledby={`nav-${group.id}`}>
            {group.items.map((item) => {
              const active = isActive(item, pathname)
              return (
                <li key={item.to}>
                  <Link
                    to={item.to}
                    className="nav-link"
                    data-status={active ? 'active' : undefined}
                    aria-current={active ? 'page' : undefined}
                    onClick={props.onNavigate}
                  >
                    <item.icon size={16} strokeWidth={1.9} aria-hidden />
                    <span>{t(item.label)}</span>
                  </Link>
                </li>
              )
            })}
          </ul>
        </div>
      ))}
    </nav>
  )
}
