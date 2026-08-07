import { useCallback, useEffect, useState } from 'react'
import { Alert, Badge, Button, Card, Skeleton, Space, Typography } from 'antd'
import { useTranslation } from 'react-i18next'

import { getTaskBoard } from '../../api/endpoints'
import { ApiError } from '../../api/errors'
import type { TaskBoardView } from '../../api/types'
import { useConnectivity } from '../../components/ConnectivityProvider'
import { TaskBoardGrid } from '../tasks/board/TaskBoardGrid'

export function DashboardTaskBoard() {
  const { t } = useTranslation()
  const connectivity = useConnectivity()
  const [board, setBoard] = useState<TaskBoardView | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError(false)
    void getTaskBoard({ signal: controller.signal })
      .then((next) => {
        if (!controller.signal.aborted) setBoard(next)
      })
      .catch((caught) => {
        if (!(caught instanceof ApiError && caught.code === 'aborted') && !controller.signal.aborted) setError(true)
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [connectivity.generation])

  const refreshBoard = useCallback(async () => {
    setBoard(await getTaskBoard())
  }, [])

  return (
    <Card
      className="dashboard-task-board"
      title={<Space><span>{t('dashboard.taskBoard')}</span><Badge count={board?.cards.length ?? 0} showZero /></Space>}
      extra={<Button type="link" href="/tasks?view=board">{t('dashboard.openTaskBoard')}</Button>}
    >
      <Typography.Paragraph type="secondary" className="dashboard-task-board__description">{t('dashboard.taskBoardDescription')}</Typography.Paragraph>
      {loading ? <Skeleton active paragraph={{ rows: 4 }} /> : null}
      {!loading && error ? <Alert type="warning" showIcon message={t('dashboard.taskBoardUnavailable')} description={t('common.retryDescription')} /> : null}
      {!loading && !error && board ? <TaskBoardGrid board={board} onBoardChange={setBoard} onRefresh={refreshBoard} testIdPrefix="dashboard-task-board" /> : null}
    </Card>
  )
}
