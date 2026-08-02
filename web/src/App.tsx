import './styles.css'

import { ConfigProvider } from 'antd'
import enUS from 'antd/locale/en_US'
import zhCN from 'antd/locale/zh_CN'
import { useLocation } from 'react-router-dom'
import { useTranslation } from 'react-i18next'

import { RouteErrorBoundary } from './components/RouteErrorBoundary'
import { AppShell } from './layouts/AppShell'
import { AppRoutes } from './routes/AppRoutes'
import { AppProviders } from './state/AppProviders'

function RoutedApplication() {
  const location = useLocation()
  const { i18n } = useTranslation()
  const locale = i18n.language === 'en-US' ? enUS : zhCN
  return (
    <ConfigProvider
      locale={locale}
      theme={{
        token: {
          colorPrimary: '#2f5bea',
          borderRadius: 10,
          fontFamily: 'Inter, "PingFang SC", "Microsoft YaHei", sans-serif',
        },
      }}
    >
      <AppShell>
        <RouteErrorBoundary key={location.pathname}>
          <AppRoutes />
        </RouteErrorBoundary>
      </AppShell>
    </ConfigProvider>
  )
}

export function App() {
  return (
    <AppProviders>
      <RoutedApplication />
    </AppProviders>
  )
}
