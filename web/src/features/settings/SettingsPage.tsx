import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { Alert, Button, Card, Input } from 'antd'
import { useTranslation } from 'react-i18next'
import { CheckCircle2, KeyRound, ShieldCheck } from 'lucide-react'

import {
  deleteProviderCredentials,
  forgetOwnerFact,
  getCodexLogin,
  getInfo,
  getNotificationStatus,
  getOwner,
  saveOwner,
  saveOwnerFact,
  saveProviderAPIKey,
  startCodexLogin,
} from '../../api/endpoints'
import type { CodexLoginSession, Info, NotificationStatus, OwnerProfile } from '../../api/types'
import { useConnectivity } from '../../components/ConnectivityProvider'
import { ErrorState } from '../../components/ErrorState'
import { PageHeader } from '../../components/PageHeader'
import { useUI } from '../../state/UIContext'
import { WeixinChannelPage } from './WeixinChannelPage'

const EMPTY_PROFILE: OwnerProfile = {
  schema_version: 1,
  identity: { display_name: '', timezone: '', language: '' },
  preferences: { communication: '', coding: '', git: '', verification: '', risk: '' },
  environments: [],
  facts: [],
}

export function SettingsPage() {
  const { t } = useTranslation()
  const connectivity = useConnectivity()
  const { actions } = useUI()
  const [info, setInfo] = useState<Info | null>(null)
  const [notification, setNotification] = useState<NotificationStatus | null>(null)
  const [profile, setProfile] = useState<OwnerProfile>(EMPTY_PROFILE)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(false)
  const [busy, setBusy] = useState(false)
  const [keys, setKeys] = useState<Record<string, string>>({})
  const [factDraft, setFactDraft] = useState({ category: '', value: '' })
  const [login, setLogin] = useState<CodexLoginSession | null>(null)
  const [selectedAccessId, setSelectedAccessId] = useState<string | null>(null)
  const mounted = useRef(true)
  const busyRef = useRef(false)

  async function reloadInfo(signal?: AbortSignal) {
    const nextInfo = await getInfo(signal === undefined ? {} : { signal })
    if (!signal?.aborted) setInfo(nextInfo)
  }

  useEffect(() => {
    mounted.current = true
    const controller = new AbortController()
    async function load() {
      try {
        const [nextInfo, nextProfile] = await Promise.all([
          getInfo({ signal: controller.signal }),
          getOwner({ signal: controller.signal }),
        ])
        let nextNotification: NotificationStatus | null = null
        try {
          nextNotification = await getNotificationStatus({ signal: controller.signal })
        } catch {
          // Older or restricted servers may not expose notification status;
          // Settings remains usable and shows the setup hint.
        }
        if (controller.signal.aborted) return
        setInfo(nextInfo)
        setProfile(nextProfile)
        setNotification(nextNotification)
        setLoadError(false)
      } catch {
        if (!controller.signal.aborted) setLoadError(true)
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    }
    void load()
    return () => {
      mounted.current = false
      controller.abort()
    }
  }, [connectivity.generation])

  useEffect(() => {
    if (login?.status !== 'pending') return
    const loginId = login.id
    const controller = new AbortController()
    async function poll() {
      try {
        const next = await getCodexLogin(loginId, { signal: controller.signal })
        if (controller.signal.aborted) return
        if (next.status === 'approved') {
          await reloadInfo(controller.signal)
          if (controller.signal.aborted) return
          actions.showToast({ messageKey: 'settings.codexApproved', tone: 'success' })
        } else if (['error', 'expired', 'cancelled'].includes(next.status)) {
          actions.showToast({ messageKey: 'settings.codexFailed', tone: 'error' })
        }
        setLogin(next)
      } catch {
        if (!controller.signal.aborted) {
          setLogin((current) => current ? { ...current, status: 'error', error: undefined } : null)
          actions.showToast({ messageKey: 'settings.codexFailed', tone: 'error' })
        }
      }
    }
    const timer = window.setTimeout(() => void poll(), 2_000)
    return () => {
      window.clearTimeout(timer)
      controller.abort()
    }
  }, [actions, login])

  const accesses = useMemo(
    () => info?.companies.flatMap((company) => company.access.map((access) => ({ company, access }))) ?? [],
    [info],
  )
  const activeAccessEntry = accesses.find(({ access }) => access.id === selectedAccessId) ?? accesses[0] ?? null
  const configuredAccessCount = accesses.filter(({ access }) => info?.auth_status[access.id]?.configured).length

  async function submitProfile(event: FormEvent) {
    event.preventDefault()
    if (!connectivity.canMutate || busyRef.current) return
    busyRef.current = true
    setBusy(true)
    try {
      const saved = await saveOwner(profile)
      if (mounted.current) setProfile(saved)
      actions.showToast({ messageKey: 'settings.profileSaved', tone: 'success' })
    } catch {
      actions.showToast({ messageKey: 'common.requestFailed', tone: 'error' })
    } finally {
      busyRef.current = false
      if (mounted.current) setBusy(false)
    }
  }

  async function addFact(event: FormEvent) {
    event.preventDefault()
    if (
      !connectivity.canMutate ||
      busyRef.current ||
      !factDraft.category.trim() ||
      !factDraft.value.trim()
    ) return
    busyRef.current = true
    setBusy(true)
    try {
      const factId = `fact-${Date.now().toString(36)}`
      const saved = await saveOwnerFact(factId, {
        category: factDraft.category.trim(),
        value: factDraft.value.trim(),
        source: 'owner-settings',
        confirmed: true,
      })
      setProfile(saved)
      setFactDraft({ category: '', value: '' })
      actions.showToast({ messageKey: 'settings.factSaved', tone: 'success' })
    } catch {
      actions.showToast({ messageKey: 'common.requestFailed', tone: 'error' })
    } finally {
      busyRef.current = false
      setBusy(false)
    }
  }

  function confirmForgetFact(factId: string) {
    actions.openDialog({
      titleKey: 'settings.forgetFactTitle',
      descriptionKey: 'settings.forgetFactDescription',
      confirmKey: 'actions.confirm',
      tone: 'warning',
      onConfirm: () => {
        if (!connectivity.canMutate || busyRef.current) return
        busyRef.current = true
        setBusy(true)
        void forgetOwnerFact(factId)
          .then((saved) => {
            if (mounted.current) setProfile(saved)
            actions.showToast({ messageKey: 'settings.factForgotten', tone: 'success' })
          })
          .catch(() => actions.showToast({ messageKey: 'common.requestFailed', tone: 'error' }))
          .finally(() => {
            busyRef.current = false
            if (mounted.current) setBusy(false)
          })
      },
    })
  }

  async function saveKey(provider: string) {
    const key = keys[provider]?.trim() ?? ''
    if (!connectivity.canMutate || busyRef.current || key === '') return
    busyRef.current = true
    setBusy(true)
    try {
      await saveProviderAPIKey(provider, key)
      actions.showToast({ messageKey: 'settings.keySaved', tone: 'success' })
      await reloadInfo()
    } catch {
      actions.showToast({ messageKey: 'common.requestFailed', tone: 'error' })
    } finally {
      busyRef.current = false
      setKeys((current) => ({ ...current, [provider]: '' }))
      setBusy(false)
    }
  }

  function confirmDeleteCredentials(provider: string) {
    actions.openDialog({
      titleKey: 'settings.deleteCredentialsTitle',
      descriptionKey: 'settings.deleteCredentialsDescription',
      confirmKey: 'actions.confirm',
      tone: 'warning',
      onConfirm: () => {
        if (!connectivity.canMutate || busyRef.current) return
        busyRef.current = true
        setBusy(true)
        void deleteProviderCredentials(provider)
          .then(() => reloadInfo())
          .then(() => actions.showToast({ messageKey: 'settings.credentialsDeleted', tone: 'success' }))
          .catch(() => actions.showToast({ messageKey: 'common.requestFailed', tone: 'error' }))
          .finally(() => {
            busyRef.current = false
            if (mounted.current) setBusy(false)
          })
      },
    })
  }

  async function beginCodexLogin() {
    if (!connectivity.canMutate || busyRef.current) return
    busyRef.current = true
    setBusy(true)
    try {
      setLogin(await startCodexLogin())
    } catch {
      actions.showToast({ messageKey: 'settings.codexFailed', tone: 'error' })
    } finally {
      busyRef.current = false
      setBusy(false)
    }
  }

  if (loading) return <p role="status">{t('common.loading')}</p>
  if (loadError && info === null) {
    return <ErrorState title={t('settings.loadError')} description={t('common.retryDescription')} />
  }

  return (
    <article className="feature-page settings-page">
      <PageHeader title={t('pages.settings.title')} description={t('settings.description')} />
      {loadError || connectivity.status === 'offline' ? <Alert className="stale-notice" type="warning" showIcon message={t('connectivity.stale')} /> : null}
      <WeixinChannelPage />
      <form className="projection-card form-grid settings-owner-card" onSubmit={(event) => void submitProfile(event)}>
        <h2>{t('settings.owner')}</h2>
        <label>
          {t('settings.displayName')}
          <Input
            value={profile.identity.display_name}
            onChange={(event) => setProfile((current) => ({
              ...current,
              identity: { ...current.identity, display_name: event.target.value },
            }))}
          />
        </label>
        <label>
          {t('settings.timezone')}
          <Input
            value={profile.identity.timezone}
            onChange={(event) => setProfile((current) => ({
              ...current,
              identity: { ...current.identity, timezone: event.target.value },
            }))}
          />
        </label>
        <label>
          {t('settings.language')}
          <Input
            value={profile.identity.language}
            onChange={(event) => setProfile((current) => ({
              ...current,
              identity: { ...current.identity, language: event.target.value },
            }))}
          />
        </label>
        <Button type="primary" htmlType="submit" disabled={!connectivity.canMutate || busy}>
          {t('settings.saveProfile')}
        </Button>
      </form>

      <Card className="projection-card settings-facts-card" variant="borderless">
        <h2>{t('settings.facts')}</h2>
        {profile.facts.length === 0 ? <p>{t('common.empty')}</p> : (
          <ul className="fact-list">
            {profile.facts.map((fact) => (
              <li key={fact.id}>
                <span><strong>{fact.category}</strong> {fact.value}</span>
                  <Button danger aria-label={t('settings.forgetFact')} onClick={() => confirmForgetFact(fact.id)}>
                  {t('settings.forgetFact')}
                </Button>
              </li>
            ))}
          </ul>
        )}
        <form className="inline-form settings-fact-form" onSubmit={(event) => void addFact(event)}>
          <label>{t('settings.factCategory')}<Input value={factDraft.category} onChange={(event) => setFactDraft((current) => ({ ...current, category: event.target.value }))} /></label>
          <label>{t('settings.factValue')}<Input value={factDraft.value} onChange={(event) => setFactDraft((current) => ({ ...current, value: event.target.value }))} /></label>
          <Button htmlType="submit" disabled={!connectivity.canMutate || busy}>{t('settings.addFact')}</Button>
        </form>
      </Card>

      <Card className="projection-card settings-connections" variant="borderless" aria-label={t('settings.providers')}>
        <header className="settings-section-heading">
          <div><span className="loop-kicker">MODEL ACCESS</span><h2>{t('settings.providers')}</h2><p>{t('settings.description')}</p></div>
          <span className="settings-connection-count"><strong>{configuredAccessCount}</strong> / {accesses.length}</span>
        </header>
        <div className="settings-provider-workbench">
          <nav className="settings-provider-list" aria-label={t('settings.providers')}>
            {accesses.map(({ company, access }) => {
              const readiness = info?.auth_status[access.id]
              const selected = activeAccessEntry?.access.id === access.id
              return <Button type="text" block className={`settings-provider-option${selected ? ' is-selected' : ''}`} aria-pressed={selected} key={access.id} onClick={() => setSelectedAccessId(access.id)}>
                <span className={`settings-provider-option__icon${readiness?.configured ? ' is-connected' : ''}`} aria-hidden="true">{readiness?.configured ? <CheckCircle2 size={18} /> : <KeyRound size={18} />}</span>
                <span className="settings-provider-option__copy"><strong>{company.label}</strong><small>{access.label}</small></span>
                <span className={`status-badge status-badge--${readiness?.configured ? 'success' : 'muted'}`}>{readiness?.configured ? t('settings.connected') : t('settings.notConnected')}</span>
              </Button>
            })}
          </nav>
          {activeAccessEntry ? (() => {
            const { company, access } = activeAccessEntry
            const readiness = info?.auth_status[access.id]
            return <article className="settings-provider-config" key={access.id}>
              <header><span className="settings-provider-config__icon"><ShieldCheck size={20} aria-hidden="true" /></span><div><span className="loop-kicker">{company.label}</span><h3>{access.label}</h3></div><span className={`status-badge status-badge--${readiness?.configured ? 'success' : 'muted'}`}>{readiness?.configured ? t('settings.connected') : t('settings.notConnected')}</span></header>
              <p className="settings-provider-config__description">{access.description || readiness?.detail}</p>
              <div className="settings-provider-readiness"><span>{readiness?.configured ? t('settings.connected') : t('settings.notConnected')}</span><strong>{readiness?.detail}</strong></div>
              {access.auth_type === 'api_key' ? <label className="settings-provider-key-field">{access.label} API Key<Input.Password autoComplete="new-password" value={keys[access.id] ?? ''} onChange={(event) => setKeys((current) => ({ ...current, [access.id]: event.target.value }))} /></label> : null}
              <footer className="settings-provider-actions">
                {access.auth_type === 'api_key' ? <Button type="primary" className="button button--primary" disabled={!connectivity.canMutate || busy || !(keys[access.id]?.trim())} onClick={() => void saveKey(access.id)}>{t('settings.saveKey')}</Button> : null}
                {access.id === 'openai-codex' && !readiness?.configured ? <Button type="primary" className="button button--primary" disabled={!connectivity.canMutate || busy} onClick={() => void beginCodexLogin()}>{t('settings.loginCodex')}</Button> : null}
                {readiness?.configured ? <Button danger className="button button--danger" disabled={!connectivity.canMutate || busy} onClick={() => confirmDeleteCredentials(access.id)}>{t('settings.deleteCredentials')}</Button> : null}
              </footer>
            </article>
          })() : <div className="settings-provider-config settings-provider-config--empty">{t('common.empty')}</div>}
        </div>
      </Card>
      <Card className="projection-card notification-settings-card" variant="borderless" aria-live="polite">
        <div className="section-heading-row">
          <div>
            <span className="loop-kicker">NOTIFY</span>
            <h2>{t('settings.notifications')}</h2>
          </div>
          <span className={`status-badge ${notification?.configured ? 'status-badge--success' : 'status-badge--muted'}`}>
            {notification?.configured ? t('settings.notificationReady') : t('settings.notificationNotConfigured')}
          </span>
        </div>
        <p>{t('settings.notificationDescription')}</p>
          <dl className="notification-status-list">
            <dt>{t('settings.notificationRecipient')}</dt><dd>{notification?.recipient ?? '1143130628@qq.com'}</dd>
            <dt>{t('settings.notificationOpenClaw')}</dt><dd>{notification?.openclaw_configured ? `${notification.openclaw_channel ?? 'openclaw-weixin'} · ${notification.openclaw_target ?? ''}` : t('settings.notificationNotConfigured')}</dd>
            {notification?.last_sent_at ? <><dt>{t('settings.notificationLastSent')}</dt><dd>{new Date(notification.last_sent_at).toLocaleString()}</dd></> : null}
          {notification?.last_error ? <><dt>{t('settings.notificationLastError')}</dt><dd className="text-danger">{notification.last_error}</dd></> : null}
        </dl>
        {!notification?.configured ? <p className="stale-notice">{t('settings.notificationSetupHint')}</p> : null}
      </Card>
      {login ? (
        <Card className="projection-card" variant="borderless" aria-live="polite">
          <h2>{t('settings.codexLogin')}</h2>
          <p>{t(`settings.loginStatus.${login.status}`)}</p>
          {login.user_code ? <strong>{login.user_code}</strong> : null}
          {login.verification_url ? <a href={login.verification_url} target="_blank" rel="noopener noreferrer">{t('settings.openLogin')}</a> : null}
          {['error', 'expired', 'cancelled'].includes(login.status) ? (
            <Button type="primary" className="button button--primary" onClick={() => void beginCodexLogin()}>{t('settings.loginAgain')}</Button>
          ) : null}
        </Card>
      ) : null}
    </article>
  )
}
