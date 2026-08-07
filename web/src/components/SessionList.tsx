import { useMemo, useState } from 'react'
import { MessageSquarePlus, Search } from 'lucide-react'
import { Button, Empty, Input, Tag, Typography } from 'antd'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'

import type { RunStatus } from '../api/types'
import { useAgentData } from '../features/agent/AgentDataContext'

function statusColor(status: RunStatus): 'success' | 'processing' | 'warning' | 'error' | 'default' {
  if (status === 'completed') return 'success'
  if (status === 'running' || status === 'verifying') return 'processing'
  if (status === 'interrupted') return 'warning'
  if (status === 'failed' || status === 'cancelled') return 'error'
  return 'default'
}

export function SessionList({ onSelect }: { onSelect?: (() => void) | undefined }) {
  const { t, i18n } = useTranslation()
  const location = useLocation()
  const navigate = useNavigate()
  const { sessions, loading, error } = useAgentData()
  const [query, setQuery] = useState('')
  const interactive = useMemo(
    () => sessions.filter((session) => session.kind !== 'employee_task'),
    [sessions],
  )
  const visible = useMemo(() => {
    const normalized = query.trim().toLocaleLowerCase(i18n.language)
    if (!normalized) return interactive
    return interactive.filter((session) => `${session.title} ${session.selection.model} ${session.selection.agent}`.toLocaleLowerCase(i18n.language).includes(normalized))
  }, [i18n.language, interactive, query])

  function createConversation() {
    void navigate('/agent')
    onSelect?.()
  }

  return (
    <div className="session-browser">
      <Button
        block
        type="primary"
        href="/agent"
        icon={<MessageSquarePlus size={17} aria-hidden="true" />}
        onClick={(event) => {
          event.preventDefault()
          createConversation()
        }}
      >
        {t('agent.newSession')}
      </Button>
      <Typography.Paragraph className="session-browser__hint" type="secondary">
        {t('session.interactiveHint')}
      </Typography.Paragraph>
      <Input
        allowClear
        aria-label={t('session.search')}
        className="session-browser__search"
        placeholder={t('session.search')}
        prefix={<Search size={16} aria-hidden="true" />}
        value={query}
        onChange={(event) => setQuery(event.target.value)}
      />
      {loading && interactive.length === 0 ? <p role="status">{t('common.loading')}</p> : null}
      {error && interactive.length === 0 ? <p role="alert">{t('session.listError')}</p> : null}
      {error ? <p className="stale-notice">{t('connectivity.stale')}</p> : null}
      {!loading && !error && interactive.length === 0 ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('session.empty')} /> : null}
      {!loading && !error && interactive.length > 0 && visible.length === 0 ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('session.noSearchResults')} /> : null}
      {visible.length > 0 ? (
        <nav className="session-list" aria-label={t('session.interactiveLabel')}>
          {visible.map((session) => {
            const path = `/agent/sessions/${encodeURIComponent(session.id)}`
            const selected = location.pathname === path
            const status = session.last_run_status ?? 'queued'
            return (
              <Link
                aria-current={selected ? 'page' : undefined}
                className={selected ? 'session-list__item is-active' : 'session-list__item'}
                key={session.id}
                onClick={onSelect}
                to={path}
              >
                <span className="session-list__heading">
                  <strong title={session.title}>{session.title}</strong>
                  <Tag color={statusColor(status)}>{t(`runStatus.${status}`)}</Tag>
                </span>
                <span className="session-list__selection">{session.selection.model || t('session.legacyModel')}</span>
                <time dateTime={session.updated_at}>
                  {new Intl.DateTimeFormat(i18n.language, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(session.updated_at))}
                </time>
              </Link>
            )
          })}
        </nav>
      ) : null}
    </div>
  )
}
