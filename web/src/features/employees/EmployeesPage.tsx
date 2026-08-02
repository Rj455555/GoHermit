import { useCallback, useEffect, useState } from 'react'
import { Button, Card, Col, Row, Select, Tag, Typography } from 'antd'
import { Plus, SlidersHorizontal, UsersRound } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'

import { listEmployees } from '../../api/endpoints'
import type { EmployeeState, EmployeeSummary } from '../../api/types'
import { useConnectivity } from '../../components/ConnectivityProvider'
import { EmptyState } from '../../components/EmptyState'
import { ErrorState } from '../../components/ErrorState'
import { PageHeader } from '../../components/PageHeader'
import { translatedEnum } from '../../i18n/enumLabel'
import { EmployeeDetailPage as Phase4EmployeeDetailPage } from './EmployeeDetailPage'
import { EmployeeWizard as Phase4EmployeeWizard } from './EmployeeWizard'

function statusLabel(t: ReturnType<typeof useTranslation>['t'], state: EmployeeState) {
  return translatedEnum(t, 'employeeStatus', state)
}

export function EmployeesPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const connectivity = useConnectivity()
  const [searchParams, setSearchParams] = useSearchParams()
  const state = searchParams.get('state') ?? ''
  const [items, setItems] = useState<EmployeeSummary[]>([])
  const [cursor, setCursor] = useState<string>()
  const [error, setError] = useState(false)
  const [wizard, setWizard] = useState(false)

  const load = useCallback(async (nextCursor?: string) => {
    try {
      const query: { state?: string; cursor?: string; limit: number } = { limit: 100 }
      if (state) query.state = state
      if (nextCursor) query.cursor = nextCursor
      const page = await listEmployees(query, {})
      setItems((current) => {
        if (!nextCursor) return page.employees
        const combined = new Map(current.map((employee) => [employee.id, employee]))
        for (const employee of page.employees) combined.set(employee.id, employee)
        return [...combined.values()]
      })
      setCursor(page.next_cursor)
      setError(false)
    } catch {
      setError(true)
    }
  }, [state])

  useEffect(() => { void load() }, [load, connectivity.generation])

  if (error && items.length === 0) {
    return (
      <ErrorState
        title={t('employees.loadError')}
        description={t('common.retryDescription')}
        action={<Button type="primary" onClick={() => void load()}>{t('actions.retry')}</Button>}
      />
    )
  }

  return (
    <article className="feature-page employees-page">
      <PageHeader
        title={t('pages.employees.title')}
        description={t('employees.description')}
        actions={(
          <Button type="primary" icon={<Plus size={16} aria-hidden="true" />} disabled={!connectivity.canMutate} onClick={() => setWizard(true)}>
            {t('employees.create')}
          </Button>
        )}
      />
      {wizard ? <Phase4EmployeeWizard onClose={() => setWizard(false)} onCreated={(record) => { void navigate(`/employees/${encodeURIComponent(record.employee.id)}`) }} /> : null}
      <div className="filter-field">
        <Typography.Text strong><SlidersHorizontal size={16} aria-hidden="true" />{t('employees.state')}</Typography.Text>
        <Select
          aria-label={t('employees.state')}
          value={state || undefined}
          placeholder={t('employees.all')}
          allowClear
          options={[
            { value: 'active', label: statusLabel(t, 'active') },
            { value: 'disabled', label: statusLabel(t, 'disabled') },
            { value: 'archived', label: statusLabel(t, 'archived') },
          ]}
          onChange={(value) => {
            const next = new URLSearchParams(searchParams)
            if (value) next.set('state', value)
            else next.delete('state')
            setSearchParams(next)
          }}
        />
      </div>
      {items.length === 0 ? (
        <EmptyState
          title={t('employees.emptyTitle')}
          description={t('employees.emptyDescription')}
          action={(
            <Button type="primary" icon={<UsersRound size={17} aria-hidden="true" />} disabled={!connectivity.canMutate} onClick={() => setWizard(true)}>
              {t('employees.createFirst')}
            </Button>
          )}
        />
      ) : (
        <Row gutter={[16, 16]} className="employee-grid">
          {items.map((employee) => (
            <Col key={employee.id} xs={24} sm={12} xl={8} xxl={6}>
              <Link className="employee-card-link" to={`/employees/${encodeURIComponent(employee.id)}`}>
                <Card hoverable className="employee-card">
                  <Typography.Title level={4} ellipsis={{ tooltip: employee.name }}>{employee.name}</Typography.Title>
                  <Typography.Paragraph type="secondary" ellipsis={{ rows: 2 }}>{employee.job_title || t('employees.jobTitle')}</Typography.Paragraph>
                  <Tag color={employee.state === 'active' ? 'success' : employee.state === 'archived' ? 'default' : 'warning'}>{statusLabel(t, employee.state)}</Tag>
                </Card>
              </Link>
            </Col>
          ))}
        </Row>
      )}
      {cursor ? <Button onClick={() => void load(cursor)}>{t('employees.loadMore')}</Button> : null}
    </article>
  )
}

export function EmployeeDetailPage() {
  return <Phase4EmployeeDetailPage />
}
