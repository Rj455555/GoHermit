import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { Button, Drawer, Layout } from 'antd'
import { Menu, PanelRightOpen } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { NavLink, useLocation } from 'react-router-dom'

import { ConfirmDialog } from '../components/ConfirmDialog'
import {
  ConnectivityBanner,
  ConnectivityProvider,
  useConnectivity,
} from '../components/ConnectivityProvider'
import { MobileSessionDrawer } from '../components/MobileSessionDrawer'
import { ToastRegion } from '../components/ToastRegion'
import { AgentDataProvider } from '../features/agent/AgentDataContext'
import { useMediaQuery } from '../hooks/useMediaQuery'
import {
  getRouteTitleKey,
  isAgentRoute,
  navigationItems,
} from '../routes/routeMeta'
import { useUI } from '../state/UIContext'
import { NavigationRail } from './NavigationRail'
import { SessionSidebar } from './SessionSidebar'
import { LanguageSwitcher } from './LanguageSwitcher'

const MOBILE_QUERY = '(max-width: 900px)'
const COMPACT_SIDER_QUERY = '(max-width: 1279px)'

export function AppShell({ children }: { children: ReactNode }) {
  const location = useLocation()
  return (
    <ConnectivityProvider>
      <AgentDataProvider active={isAgentRoute(location.pathname)}>
        <AppShellFrame>{children}</AppShellFrame>
      </AgentDataProvider>
    </ConnectivityProvider>
  )
}

function AppShellFrame({ children }: { children: ReactNode }) {
  const { t } = useTranslation()
  const location = useLocation()
  const { state, actions } = useUI()
  const connectivity = useConnectivity()
  const mobile = useMediaQuery(MOBILE_QUERY)
  const compactSider = useMediaQuery(COMPACT_SIDER_QUERY)
  const siderCollapsed = state.navigationCollapsed || compactSider
  const agentRoute = isAgentRoute(location.pathname)
  const restoreSidebarRef = useRef<HTMLButtonElement>(null)
  const drawerTriggerRef = useRef<HTMLButtonElement>(null)
  const pendingSidebarFocusRef = useRef(false)
  const [mobileNavigationOpen, setMobileNavigationOpen] = useState(false)

  const closeDrawer = useCallback(
    () => actions.setMobileSessionDrawerOpen(false),
    [actions],
  )

  useEffect(() => {
    document.title = `${t(getRouteTitleKey(location.pathname))} · GoHermit`
  }, [location.pathname, state.locale, t])

  useEffect(() => {
    closeDrawer()
    setMobileNavigationOpen(false)
  }, [closeDrawer, location.pathname, mobile])

  useLayoutEffect(() => {
    if (
      pendingSidebarFocusRef.current &&
      agentRoute &&
      !mobile &&
      state.sessionSidebarCollapsed
    ) {
      pendingSidebarFocusRef.current = false
      restoreSidebarRef.current?.focus()
    }
  }, [agentRoute, mobile, state.sessionSidebarCollapsed])

  const drawerOpen = agentRoute && mobile && state.mobileSessionDrawerOpen
  const shellIsolated = drawerOpen || mobileNavigationOpen || state.dialog !== null

  const collapseSessionSidebar = useCallback(() => {
    pendingSidebarFocusRef.current = true
    actions.setSessionSidebarCollapsed(true)
  }, [actions])

  return (
    <div className="app-shell" data-testid="react-bootstrap">
      <a className="skip-link" href="#main-content">
        {t('navigation.skipToContent')}
      </a>
      <div
        className="app-shell__background"
        data-testid="shell-background"
        inert={shellIsolated}
      >
        <Layout className="app-shell__antd-layout" hasSider>
          {!mobile ? (
            <Layout.Sider
              className="app-shell__sider"
              collapsed={siderCollapsed}
              collapsedWidth={68}
              width={228}
              trigger={null}
            >
              <NavigationRail collapsed={siderCollapsed} allowToggle={!compactSider} />
            </Layout.Sider>
          ) : null}
          <Layout className="app-shell__workspace">
          <ConnectivityBanner />
          {mobile ? (
            <Layout.Header className="mobile-bar">
              <span className="navigation-rail__mark" aria-hidden="true">
                GH
              </span>
              <Button
                type="text"
                htmlType="button"
                className="mobile-bar__trigger"
                aria-label={t('navigation.label')}
                aria-expanded={mobileNavigationOpen}
                icon={<Menu size={21} aria-hidden="true" />}
                onClick={() => setMobileNavigationOpen((open) => !open)}
              />
            </Layout.Header>
          ) : null}
          {agentRoute && mobile ? (
            <div className="mobile-session-toolbar">
              <button
                ref={drawerTriggerRef}
                type="button"
                className="button button--secondary"
                aria-label={t('session.openDrawer')}
                aria-expanded={drawerOpen}
                onClick={() => actions.setMobileSessionDrawerOpen(true)}
              >
                <PanelRightOpen size={17} aria-hidden="true" />
                <span>{t('session.label')}</span>
              </button>
            </div>
          ) : null}
          <div className="review-bar" aria-label={t('brand.subtitle')}>
            <div className="review-note">
              <span className={`review-dot${connectivity.status === 'offline' ? ' review-dot--offline' : ''}`} aria-hidden="true" />
              <strong>{t('brand.subtitle')}</strong>
              <span className="review-note__detail">· {connectivity.status === 'offline' ? t('connectivity.offline') : 'API-aware · live projection'}</span>
            </div>
            <div className="review-controls">
              <label>
                <span>{t('employees.state')}</span>
                <select
                  aria-label={t('brand.subtitle')}
                  value={connectivity.status === 'offline' ? 'offline' : 'default'}
                  onChange={() => undefined}
                >
                  <option value="default">{state.locale === 'zh-CN' ? '默认' : 'Default'}</option>
                  <option value="offline">{t('connectivity.offline')}</option>
                </select>
              </label>
              <LanguageSwitcher />
            </div>
          </div>
          <Layout.Content id="main-content" className="app-shell__content" tabIndex={-1}>{children}</Layout.Content>
          {agentRoute && !mobile && !state.sessionSidebarCollapsed ? (
            <SessionSidebar onCollapse={collapseSessionSidebar} />
          ) : null}
          {agentRoute && !mobile && state.sessionSidebarCollapsed ? (
            <button
              ref={restoreSidebarRef}
              type="button"
              className="session-sidebar-restore"
              aria-label={t('session.expand')}
              title={t('session.expand')}
              onClick={() => actions.setSessionSidebarCollapsed(false)}
            >
              <PanelRightOpen size={18} aria-hidden="true" />
            </button>
          ) : null}
          </Layout>
        </Layout>
      </div>
      <Drawer
        title={t('navigation.label')}
        placement="left"
        width="min(88vw, 360px)"
        open={mobile && mobileNavigationOpen}
        onClose={() => setMobileNavigationOpen(false)}
        className="mobile-navigation-drawer"
      >
        <nav className="mobile-navigation" aria-label={t('navigation.label')}>
          {navigationItems.map(({ to, labelKey, icon: Icon }) => (
            <NavLink
              key={to}
              to={to}
              end={to === '/dashboard'}
              className={({ isActive }) =>
                `mobile-navigation__link${isActive ? ' mobile-navigation__link--active' : ''}`
              }
            >
              <Icon size={18} strokeWidth={1.8} aria-hidden="true" />
              <span>{t(labelKey)}</span>
            </NavLink>
          ))}
        </nav>
      </Drawer>
      <MobileSessionDrawer
        open={drawerOpen}
        onClose={closeDrawer}
        returnFocus={drawerTriggerRef}
      />
      <ToastRegion />
      <ConfirmDialog />
    </div>
  )
}
