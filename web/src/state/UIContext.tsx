import {
  createContext,
  useCallback,
  useContext,
  useLayoutEffect,
  useMemo,
  useReducer,
  useRef,
  type ReactNode,
} from 'react'

import { i18n } from '../i18n/i18n'
import type { Locale } from '../i18n/resources'
import {
  readStoredBoolean,
  readStoredLocale,
  STORAGE_KEYS,
  writeStoredBoolean,
  writeStoredLocale,
} from './storage'

export type FeedbackTone = 'info' | 'success' | 'warning' | 'error'

export interface ToastState {
  id: number
  messageKey: string
  tone: FeedbackTone
}

export interface DialogState {
  titleKey: string
  descriptionKey: string
  confirmKey: string
  tone?: FeedbackTone
  onConfirm: () => void
}

export interface UIState {
  locale: Locale
  navigationCollapsed: boolean
  sessionSidebarCollapsed: boolean
  mobileSessionDrawerOpen: boolean
  toast: ToastState | null
  dialog: DialogState | null
}

type UIAction =
  | { type: 'set-locale'; locale: Locale }
  | { type: 'set-navigation-collapsed'; collapsed: boolean }
  | { type: 'set-session-sidebar-collapsed'; collapsed: boolean }
  | { type: 'set-mobile-session-drawer-open'; open: boolean }
  | { type: 'show-toast'; toast: ToastState }
  | { type: 'dismiss-toast' }
  | { type: 'open-dialog'; dialog: DialogState }
  | { type: 'close-dialog' }

function initializeUIState(): UIState {
  return {
    locale: readStoredLocale(),
    navigationCollapsed: readStoredBoolean(STORAGE_KEYS.navigationCollapsed),
    sessionSidebarCollapsed: readStoredBoolean(STORAGE_KEYS.sessionSidebarCollapsed),
    mobileSessionDrawerOpen: false,
    toast: null,
    dialog: null,
  }
}

function reducer(state: UIState, action: UIAction): UIState {
  switch (action.type) {
    case 'set-locale':
      return { ...state, locale: action.locale }
    case 'set-navigation-collapsed':
      return { ...state, navigationCollapsed: action.collapsed }
    case 'set-session-sidebar-collapsed':
      return { ...state, sessionSidebarCollapsed: action.collapsed }
    case 'set-mobile-session-drawer-open':
      return { ...state, mobileSessionDrawerOpen: action.open }
    case 'show-toast':
      return { ...state, toast: action.toast }
    case 'dismiss-toast':
      return { ...state, toast: null }
    case 'open-dialog':
      return { ...state, dialog: action.dialog }
    case 'close-dialog':
      return { ...state, dialog: null }
  }
}

interface UIActions {
  setLocale: (locale: Locale) => void
  setNavigationCollapsed: (collapsed: boolean) => void
  setSessionSidebarCollapsed: (collapsed: boolean) => void
  setMobileSessionDrawerOpen: (open: boolean) => void
  showToast: (toast: Omit<ToastState, 'id'>) => void
  dismissToast: () => void
  openDialog: (dialog: DialogState) => void
  closeDialog: () => void
}

interface UIContextValue {
  state: UIState
  actions: UIActions
}

const UIContext = createContext<UIContextValue | undefined>(undefined)

export function UIProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(reducer, undefined, initializeUIState)
  const nextToastID = useRef(0)

  useLayoutEffect(() => {
    document.documentElement.lang = state.locale
    if (i18n.resolvedLanguage !== state.locale) {
      void i18n.changeLanguage(state.locale)
    }
  }, [state.locale])

  const setLocale = useCallback((locale: Locale) => {
    writeStoredLocale(locale)
    dispatch({ type: 'set-locale', locale })
  }, [])

  const setNavigationCollapsed = useCallback((collapsed: boolean) => {
    writeStoredBoolean(STORAGE_KEYS.navigationCollapsed, collapsed)
    dispatch({ type: 'set-navigation-collapsed', collapsed })
  }, [])

  const setSessionSidebarCollapsed = useCallback((collapsed: boolean) => {
    writeStoredBoolean(STORAGE_KEYS.sessionSidebarCollapsed, collapsed)
    dispatch({ type: 'set-session-sidebar-collapsed', collapsed })
  }, [])

  const setMobileSessionDrawerOpen = useCallback((open: boolean) => {
    dispatch({ type: 'set-mobile-session-drawer-open', open })
  }, [])

  const showToast = useCallback((toast: Omit<ToastState, 'id'>) => {
    nextToastID.current += 1
    dispatch({ type: 'show-toast', toast: { ...toast, id: nextToastID.current } })
  }, [])

  const dismissToast = useCallback(() => dispatch({ type: 'dismiss-toast' }), [])
  const openDialog = useCallback(
    (dialog: DialogState) => dispatch({ type: 'open-dialog', dialog }),
    [],
  )
  const closeDialog = useCallback(() => dispatch({ type: 'close-dialog' }), [])

  const actions = useMemo<UIActions>(
    () => ({
      setLocale,
      setNavigationCollapsed,
      setSessionSidebarCollapsed,
      setMobileSessionDrawerOpen,
      showToast,
      dismissToast,
      openDialog,
      closeDialog,
    }),
    [
      closeDialog,
      dismissToast,
      openDialog,
      setLocale,
      setMobileSessionDrawerOpen,
      setNavigationCollapsed,
      setSessionSidebarCollapsed,
      showToast,
    ],
  )

  const value = useMemo(() => ({ state, actions }), [actions, state])
  return <UIContext.Provider value={value}>{children}</UIContext.Provider>
}

// eslint-disable-next-line react-refresh/only-export-components -- Provider and its hook form one API.
export function useUI(): UIContextValue {
  const context = useContext(UIContext)
  if (context === undefined) throw new Error('useUI must be used within UIProvider')
  return context
}
