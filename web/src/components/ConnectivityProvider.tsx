import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from 'antd'

import { getHealth } from '../api/endpoints'

type ConnectivityStatus = 'checking' | 'online' | 'offline'

interface ConnectivityValue {
  status: ConnectivityStatus
  generation: number
  canMutate: boolean
  reconnect: () => void
}

const ConnectivityContext = createContext<ConnectivityValue | undefined>(undefined)
const HEARTBEAT_INTERVAL_MS = 30_000

export function ConnectivityProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<ConnectivityStatus>('checking')
  const [generation, setGeneration] = useState(0)
  const statusRef = useRef(status)
  const requestRef = useRef<AbortController | null>(null)

  useEffect(() => {
    statusRef.current = status
  }, [status])

  const check = useCallback(async (recovery: boolean) => {
    requestRef.current?.abort()
    const controller = new AbortController()
    requestRef.current = controller
    try {
      await getHealth({ signal: controller.signal })
      if (controller.signal.aborted) return
      const recovered = statusRef.current === 'offline'
      statusRef.current = 'online'
      setStatus('online')
      if (recovery || recovered) setGeneration((value) => value + 1)
    } catch {
      if (controller.signal.aborted) return
      statusRef.current = 'offline'
      setStatus('offline')
    }
  }, [])

  useEffect(() => {
    void check(false)
    const timer = window.setInterval(() => void check(false), HEARTBEAT_INTERVAL_MS)
    return () => {
      window.clearInterval(timer)
      requestRef.current?.abort()
    }
  }, [check])

  const reconnect = useCallback(() => void check(true), [check])
  const value = useMemo<ConnectivityValue>(
    () => ({ status, generation, canMutate: status === 'online', reconnect }),
    [generation, reconnect, status],
  )
  return <ConnectivityContext.Provider value={value}>{children}</ConnectivityContext.Provider>
}

export function ConnectivityBanner() {
  const { t } = useTranslation()
  const { status, reconnect } = useConnectivity()
  if (status !== 'offline') return null
  return (
    <div className="connectivity-banner" role="alert">
      <span>{t('connectivity.offline')}</span>
      <Button type="default" className="button button--secondary" onClick={reconnect}>
        {t('connectivity.reconnect')}
      </Button>
    </div>
  )
}

// eslint-disable-next-line react-refresh/only-export-components -- Provider and hook form one API.
export function useConnectivity(): ConnectivityValue {
  const value = useContext(ConnectivityContext)
  if (value === undefined) throw new Error('useConnectivity must be used within ConnectivityProvider')
  return value
}
