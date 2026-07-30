import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import {
  deleteProviderCredentials,
  forgetOwnerFact,
  getCodexLogin,
  getInfo,
  getOwner,
  saveOwner,
  saveOwnerFact,
  saveProviderAPIKey,
  startCodexLogin,
} from '../../api/endpoints'
import type { CodexLoginSession, Info, OwnerProfile } from '../../api/types'
import { useConnectivity } from '../../components/ConnectivityProvider'
import { ErrorState } from '../../components/ErrorState'
import { PageHeader } from '../../components/PageHeader'
import { useUI } from '../../state/UIContext'

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
  const [profile, setProfile] = useState<OwnerProfile>(EMPTY_PROFILE)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(false)
  const [busy, setBusy] = useState(false)
  const [keys, setKeys] = useState<Record<string, string>>({})
  const [factDraft, setFactDraft] = useState({ category: '', value: '' })
  const [login, setLogin] = useState<CodexLoginSession | null>(null)
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
        if (controller.signal.aborted) return
        setInfo(nextInfo)
        setProfile(nextProfile)
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
      {loadError || connectivity.status === 'offline' ? <p className="stale-notice">{t('connectivity.stale')}</p> : null}
      <form className="projection-card form-grid" onSubmit={(event) => void submitProfile(event)}>
        <h2>{t('settings.owner')}</h2>
        <label>
          {t('settings.displayName')}
          <input
            value={profile.identity.display_name}
            onChange={(event) => setProfile((current) => ({
              ...current,
              identity: { ...current.identity, display_name: event.target.value },
            }))}
          />
        </label>
        <label>
          {t('settings.timezone')}
          <input
            value={profile.identity.timezone}
            onChange={(event) => setProfile((current) => ({
              ...current,
              identity: { ...current.identity, timezone: event.target.value },
            }))}
          />
        </label>
        <label>
          {t('settings.language')}
          <input
            value={profile.identity.language}
            onChange={(event) => setProfile((current) => ({
              ...current,
              identity: { ...current.identity, language: event.target.value },
            }))}
          />
        </label>
        <button className="button button--primary" type="submit" disabled={!connectivity.canMutate || busy}>
          {t('settings.saveProfile')}
        </button>
      </form>

      <section className="projection-card">
        <h2>{t('settings.facts')}</h2>
        {profile.facts.length === 0 ? <p>{t('common.empty')}</p> : (
          <ul className="fact-list">
            {profile.facts.map((fact) => (
              <li key={fact.id}>
                <span><strong>{fact.category}</strong> {fact.value}</span>
                <button type="button" className="button button--danger" onClick={() => confirmForgetFact(fact.id)}>
                  {t('settings.forgetFact')}
                </button>
              </li>
            ))}
          </ul>
        )}
        <form className="inline-form" onSubmit={(event) => void addFact(event)}>
          <label>{t('settings.factCategory')}<input value={factDraft.category} onChange={(event) => setFactDraft((current) => ({ ...current, category: event.target.value }))} /></label>
          <label>{t('settings.factValue')}<input value={factDraft.value} onChange={(event) => setFactDraft((current) => ({ ...current, value: event.target.value }))} /></label>
          <button type="submit" className="button button--secondary" disabled={!connectivity.canMutate || busy}>{t('settings.addFact')}</button>
        </form>
      </section>

      <section className="provider-grid" aria-label={t('settings.providers')}>
        {accesses.map(({ company, access }) => {
          const readiness = info?.auth_status[access.id]
          return (
            <article className="projection-card provider-card" key={access.id}>
              <h2>{company.label} · {access.label}</h2>
              <p>{readiness?.configured ? t('settings.connected') : t('settings.notConnected')}</p>
              <small>{readiness?.detail}</small>
              {access.auth_type === 'api_key' ? (
                <div className="inline-form">
                  <label>
                    {access.label} API Key
                    <input
                      type="password"
                      autoComplete="off"
                      value={keys[access.id] ?? ''}
                      onChange={(event) => setKeys((current) => ({ ...current, [access.id]: event.target.value }))}
                    />
                  </label>
                  <button type="button" className="button button--primary" disabled={!connectivity.canMutate || busy} onClick={() => void saveKey(access.id)}>
                    {t('settings.saveKey')}
                  </button>
                </div>
              ) : access.id === 'openai-codex' && !readiness?.configured ? (
                <button type="button" className="button button--primary" disabled={!connectivity.canMutate || busy} onClick={() => void beginCodexLogin()}>
                  {t('settings.loginCodex')}
                </button>
              ) : null}
              {readiness?.configured ? (
                <button type="button" className="button button--danger" disabled={!connectivity.canMutate || busy} onClick={() => confirmDeleteCredentials(access.id)}>
                  {t('settings.deleteCredentials')}
                </button>
              ) : null}
            </article>
          )
        })}
      </section>
      {login ? (
        <section className="projection-card" aria-live="polite">
          <h2>{t('settings.codexLogin')}</h2>
          <p>{t(`settings.loginStatus.${login.status}`)}</p>
          {login.user_code ? <strong>{login.user_code}</strong> : null}
          {login.verification_url ? <a href={login.verification_url} target="_blank" rel="noopener noreferrer">{t('settings.openLogin')}</a> : null}
          {['error', 'expired', 'cancelled'].includes(login.status) ? (
            <button type="button" className="button button--primary" onClick={() => void beginCodexLogin()}>{t('settings.loginAgain')}</button>
          ) : null}
        </section>
      ) : null}
    </article>
  )
}
