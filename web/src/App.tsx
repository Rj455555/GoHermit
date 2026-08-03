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
          colorBgLayout: '#f3f5f7',
          colorBgContainer: '#ffffff',
          colorBorderSecondary: '#e6eaf0',
          colorText: '#172033',
          colorTextSecondary: '#64748b',
          borderRadius: 8,
          fontSize: 14,
          controlHeight: 40,
          boxShadowSecondary: '0 12px 32px rgb(15 23 42 / 8%)',
          motionDurationMid: '0.2s',
          motionDurationSlow: '0.3s',
          fontFamily: 'Inter, "PingFang SC", "Microsoft YaHei", sans-serif',
        },
        components: {
          Layout: {
            headerBg: '#ffffff',
            bodyBg: '#f3f5f7',
            siderBg: '#172033',
          },
          Menu: {
            darkItemBg: '#172033',
            darkItemColor: '#cbd5e1',
            darkItemHoverBg: '#24324b',
            darkItemSelectedBg: '#2f5bea',
            darkItemSelectedColor: '#ffffff',
          },
          Card: {
            headerFontSize: 16,
            bodyPadding: 20,
          },
          Table: {
            headerBg: '#f8fafc',
            rowHoverBg: '#f5f8ff',
          },
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
