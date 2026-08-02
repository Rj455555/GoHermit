import { ChevronLeft, ChevronRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { NavLink } from 'react-router-dom'

import { navigationItems } from '../routes/routeMeta'
import { useUI } from '../state/UIContext'
import { LanguageSwitcher } from './LanguageSwitcher'

export function NavigationRail() {
  const { t } = useTranslation()
  const { state, actions } = useUI()
  const collapsed = state.navigationCollapsed
  const toggleLabel = t(collapsed ? 'navigation.expand' : 'navigation.collapse')

  return (
    <nav
      className="navigation-rail"
      aria-label={t('navigation.label')}
      data-collapsed={String(collapsed)}
    >
      <div className="navigation-rail__brand" title={t('brand.label')}>
        <span className="navigation-rail__mark" aria-hidden="true">
          GH
        </span>
        {collapsed ? null : (
          <span className="navigation-rail__brand-copy">
            <strong className="navigation-rail__brand-name" aria-label="GoHermit" />
            <span className="sr-only">{t('brand.label')}</span>
            <small>{t('brand.subtitle')}</small>
          </span>
        )}
      </div>
      <ul className="navigation-rail__links">
        {navigationItems.map(({ to, labelKey, icon: Icon }) => (
          <li key={to}>
            <NavLink
              to={to}
              end={to === '/dashboard'}
              title={t(labelKey)}
              aria-label={t(labelKey)}
              className={({ isActive }) =>
                `navigation-rail__link${isActive ? ' navigation-rail__link--active' : ''}`
              }
            >
              <Icon size={18} strokeWidth={1.8} aria-hidden="true" />
              {collapsed ? null : <span>{t(labelKey)}</span>}
            </NavLink>
          </li>
        ))}
      </ul>
      <div className="navigation-rail__footer">
        <LanguageSwitcher compact={collapsed} />
        <button
          type="button"
          className="navigation-rail__toggle"
          aria-label={toggleLabel}
          title={toggleLabel}
          aria-expanded={!collapsed}
          onClick={() => actions.setNavigationCollapsed(!collapsed)}
        >
          {collapsed ? (
            <ChevronRight aria-hidden="true" size={18} />
          ) : (
            <>
              <ChevronLeft aria-hidden="true" size={18} />
              <span>{toggleLabel}</span>
            </>
          )}
        </button>
      </div>
    </nav>
  )
}
