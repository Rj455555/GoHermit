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

import { getInfo, listSessions } from '../../api/endpoints'
import type { Info, SessionSummary } from '../../api/types'
import { useConnectivity } from '../../components/ConnectivityProvider'

interface AgentDataValue {
  info: Info | null
  sessions: SessionSummary[]
  loading: boolean
  error: boolean
  refresh: () => Promise<void>
}

const AgentDataContext = createContext<AgentDataValue | undefined>(undefined)

export function AgentDataProvider({
  active,
  children,
}: {
  active: boolean
  children: ReactNode
}) {
  const connectivity = useConnectivity()
  const [info, setInfo] = useState<Info | null>(null)
  const [sessions, setSessions] = useState<SessionSummary[]>([])
  const [loading, setLoading] = useState(active)
  const [error, setError] = useState(false)
  const controllerRef = useRef<AbortController | null>(null)
  const requestVersion = useRef(0)

  const refresh = useCallback(async () => {
    if (!active) return
    controllerRef.current?.abort()
    const controller = new AbortController()
    controllerRef.current = controller
    requestVersion.current += 1
    const version = requestVersion.current
    setLoading(true)
    try {
      const [nextInfo, response] = await Promise.all([
        getInfo({ signal: controller.signal }),
        listSessions({ signal: controller.signal }),
      ])
      if (controller.signal.aborted || requestVersion.current !== version) return
      setInfo(nextInfo)
      setSessions([...response.sessions].sort(
        (left, right) => Date.parse(right.updated_at) - Date.parse(left.updated_at),
      ))
      setError(false)
    } catch {
      if (!controller.signal.aborted) setError(true)
    } finally {
      if (!controller.signal.aborted && requestVersion.current === version) setLoading(false)
    }
  }, [active])

  useEffect(() => {
    if (active) void refresh()
    return () => controllerRef.current?.abort()
  }, [active, connectivity.generation, refresh])

  const value = useMemo(
    () => ({ info, sessions, loading, error, refresh }),
    [error, info, loading, refresh, sessions],
  )
  return <AgentDataContext.Provider value={value}>{children}</AgentDataContext.Provider>
}

// eslint-disable-next-line react-refresh/only-export-components -- Provider and hook form one API.
export function useAgentData(): AgentDataValue {
  const value = useContext(AgentDataContext)
  if (value === undefined) throw new Error('useAgentData must be used within AgentDataProvider')
  return value
}
