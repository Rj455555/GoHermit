import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  type ReactNode,
} from 'react'
import { PanelRightOpen } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useLocation } from 'react-router-dom'

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
import { getRouteTitleKey, isAgentRoute } from '../routes/routeMeta'
import { useUI } from '../state/UIContext'
import { NavigationRail } from './NavigationRail'
import { SessionSidebar } from './SessionSidebar'
import { LanguageSwitcher } from './LanguageSwitcher'

const MOBILE_QUERY = '(max-width: 900px)'

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
  const agentRoute = isAgentRoute(location.pathname)
  const restoreSidebarRef = useRef<HTMLButtonElement>(null)
  const drawerTriggerRef = useRef<HTMLButtonElement>(null)
  const pendingSidebarFocusRef = useRef(false)

  const closeDrawer = useCallback(
    () => actions.setMobileSessionDrawerOpen(false),
    [actions],
  )

  useEffect(() => {
    document.title = `${t(getRouteTitleKey(location.pathname))} · GoHermit`
  }, [location.pathname, state.locale, t])

  useEffect(() => {
    closeDrawer()
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
  const shellIsolated = drawerOpen || state.dialog !== null

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
        <NavigationRail />
        <ConnectivityBanner />
        <div className="app-shell__workspace">
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
              <span className="review-controls__label">{t('employees.state')}</span>
              <span className={`review-state review-state--${connectivity.status === 'offline' ? 'offline' : 'ready'}`}>
                {connectivity.status === 'offline' ? t('connectivity.reconnect') : t('dashboard.idle')}
              </span>
              <LanguageSwitcher />
            </div>
          </div>
          <main id="main-content" className="app-shell__content" tabIndex={-1}>{children}</main>
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
        </div>
      </div>
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
