import { useTranslation } from 'react-i18next'
import { Link, useLocation } from 'react-router-dom'

import { useAgentData } from '../features/agent/AgentDataContext'

export function SessionList({ onSelect }: { onSelect?: (() => void) | undefined }) {
  const { t, i18n } = useTranslation()
  const location = useLocation()
  const { sessions, loading, error } = useAgentData()
  return (
    <>
      <Link className="button button--primary session-new-link" to="/agent" onClick={onSelect}>
        {t('agent.newSession')}
      </Link>
      {loading && sessions.length === 0 ? <p role="status">{t('common.loading')}</p> : null}
      {error && sessions.length === 0 ? <p role="alert">{t('session.listError')}</p> : null}
      {error ? <p className="stale-notice">{t('connectivity.stale')}</p> : null}
      {!loading && !error && sessions.length === 0 ? <p>{t('session.empty')}</p> : null}
      {sessions.length > 0 ? (
        <nav className="session-list" aria-label={t('session.label')}>
          {sessions.map((session) => {
            const path = `/agent/sessions/${encodeURIComponent(session.id)}`
            const selected = location.pathname === path
            return (
              <Link
                aria-current={selected ? 'page' : undefined}
                className={selected ? 'session-list__item is-active' : 'session-list__item'}
                key={session.id}
                onClick={onSelect}
                to={path}
              >
                <strong>{session.title}</strong>
                <span>{t(`runStatus.${session.last_run_status ?? 'queued'}`)}</span>
                <time dateTime={session.updated_at}>
                  {new Intl.DateTimeFormat(i18n.language, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(session.updated_at))}
                </time>
              </Link>
            )
          })}
        </nav>
      ) : null}
    </>
  )
}
