import { useEffect, useMemo, useRef, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Form,
  Input,
  List,
  Modal,
  Select,
  Space,
  Tag,
  Typography,
} from 'antd'
import { useTranslation } from 'react-i18next'

import {
  cancelWeixinLogin,
  deleteWeixinBinding,
  getWeixinAccounts,
  getWeixinBindings,
  getWeixinInbox,
  getWeixinLoginStatus,
  listEmployees,
  logoutWeixinAccount,
  saveWeixinBinding,
  startWeixinLogin,
} from '../../api/endpoints'
import type { WeixinAccount, WeixinBinding, WeixinInboxItem, WeixinLoginAttempt } from '../../api/types'
import { useConnectivity } from '../../components/ConnectivityProvider'
import { useUI } from '../../state/UIContext'

const stateColor: Record<WeixinAccount['state'], string> = {
  connected: 'success',
  confirmed: 'success',
  scanned: 'processing',
  qr_pending: 'processing',
  reconnecting: 'warning',
  expired: 'error',
  failed: 'error',
  disconnected: 'default',
  logged_out: 'default',
}
const LOGIN_ATTEMPT_STORAGE_KEY = 'gohermit.weixin.loginAttempt'

export function WeixinChannelPage() {
  const { t } = useTranslation()
  const connectivity = useConnectivity()
  const { actions } = useUI()
  const [accounts, setAccounts] = useState<WeixinAccount[]>([])
  const [bindings, setBindings] = useState<WeixinBinding[]>([])
  const [employees, setEmployees] = useState<Array<{ id: string; name: string }>>([])
  const [inbox, setInbox] = useState<WeixinInboxItem[]>([])
  const [inboxAccountID, setInboxAccountID] = useState('')
  const [inboxBusy, setInboxBusy] = useState(false)
  const [attempt, setAttempt] = useState<WeixinLoginAttempt | null>(null)
  const [secondsRemaining, setSecondsRemaining] = useState(0)
  const [busy, setBusy] = useState(false)
  const [bindingForm] = Form.useForm()
  const mounted = useRef(true)

  async function reload() {
    const [nextAccounts, nextBindings, employeePage] = await Promise.all([
      getWeixinAccounts(),
      getWeixinBindings(),
      listEmployees({ limit: 100 }),
    ])
    if (!mounted.current) return
    setAccounts(nextAccounts.accounts)
    setBindings(nextBindings.bindings)
    setEmployees(employeePage.employees.map((employee) => ({ id: employee.id, name: employee.name })))
  }

  useEffect(() => {
    mounted.current = true
    void reload().catch(() => actions.showToast({ messageKey: 'common.requestFailed', tone: 'error' }))
    return () => {
      mounted.current = false
    }
  }, [actions])

  useEffect(() => {
    const attemptID = window.localStorage.getItem(LOGIN_ATTEMPT_STORAGE_KEY)
    if (!attemptID) return
    const controller = new AbortController()
    void getWeixinLoginStatus(attemptID, { signal: controller.signal })
      .then((savedAttempt) => {
        if (controller.signal.aborted) return
        if (['connected', 'expired', 'disconnected', 'failed', 'logged_out'].includes(savedAttempt.state)) {
          window.localStorage.removeItem(LOGIN_ATTEMPT_STORAGE_KEY)
          return
        }
        setAttempt(savedAttempt)
      })
      .catch(() => {
        if (!controller.signal.aborted) window.localStorage.removeItem(LOGIN_ATTEMPT_STORAGE_KEY)
      })
    return () => controller.abort()
  }, [])

  useEffect(() => {
    if (!attempt || ['connected', 'expired', 'disconnected', 'failed', 'logged_out'].includes(attempt.state)) return
    const controller = new AbortController()
    const timer = window.setTimeout(() => {
      void getWeixinLoginStatus(attempt.id, { signal: controller.signal })
        .then((next) => {
          if (controller.signal.aborted) return
          setAttempt(next)
          if (next.state === 'connected') {
            window.localStorage.removeItem(LOGIN_ATTEMPT_STORAGE_KEY)
            actions.showToast({ messageKey: 'settings.weixinConnected', tone: 'success' })
            void reload()
          }
        })
        .catch(() => {
          if (!controller.signal.aborted) actions.showToast({ messageKey: 'common.requestFailed', tone: 'error' })
        })
    }, 1500)
    return () => {
      window.clearTimeout(timer)
      controller.abort()
    }
  }, [actions, attempt])

  useEffect(() => {
    if (!attempt) {
      setSecondsRemaining(0)
      return
    }
    const update = () => setSecondsRemaining(Math.max(0, Math.ceil((Date.parse(attempt.expires_at) - Date.now()) / 1000)))
    update()
    const timer = window.setInterval(update, 1000)
    return () => window.clearInterval(timer)
  }, [attempt])

  const accountOptions = useMemo(
    () => accounts.map((account) => ({ label: account.label || account.id, value: account.id })),
    [accounts],
  )

  async function addAccount() {
    if (!connectivity.canMutate || busy) return
    setBusy(true)
    try {
      const next = await startWeixinLogin({})
      setAttempt(next)
      window.localStorage.setItem(LOGIN_ATTEMPT_STORAGE_KEY, next.id)
    } catch {
      actions.showToast({ messageKey: 'settings.weixinLoginFailed', tone: 'error' })
    } finally {
      setBusy(false)
    }
  }

  async function refreshLogin() {
    if (!attempt || busy) return
    setBusy(true)
    try {
      const next = await startWeixinLogin({ account_id: attempt.account_id })
      setAttempt(next)
      window.localStorage.setItem(LOGIN_ATTEMPT_STORAGE_KEY, next.id)
    } catch {
      actions.showToast({ messageKey: 'settings.weixinLoginFailed', tone: 'error' })
    } finally {
      setBusy(false)
    }
  }

  async function cancelLogin() {
    if (!attempt) return
    try {
      await cancelWeixinLogin(attempt.id)
      setAttempt(null)
      window.localStorage.removeItem(LOGIN_ATTEMPT_STORAGE_KEY)
    } catch {
      actions.showToast({ messageKey: 'common.requestFailed', tone: 'error' })
    }
  }

  async function loadInbox(accountID = inboxAccountID) {
    if (!accountID || inboxBusy) return
    setInboxBusy(true)
    try {
      const result = await getWeixinInbox(accountID)
      if (mounted.current) setInbox(result.items)
    } catch {
      actions.showToast({ messageKey: 'common.requestFailed', tone: 'error' })
    } finally {
      if (mounted.current) setInboxBusy(false)
    }
  }

  function maskedPeer(value: string) {
    if (value.length <= 4) return '••••'
    return value.slice(0, 2) + '••••' + value.slice(-2)
  }

  function confirmLogout(account: WeixinAccount) {
    actions.openDialog({
      titleKey: 'settings.weixinLogoutTitle',
      descriptionKey: 'settings.weixinLogoutDescription',
      confirmKey: 'actions.confirm',
      tone: 'warning',
      onConfirm: () => {
        void logoutWeixinAccount(account.id)
          .then(() => reload())
          .catch(() => actions.showToast({ messageKey: 'common.requestFailed', tone: 'error' }))
      },
    })
  }

  async function submitBinding(values: { account_id: string; peer_id?: string; group_id?: string; employee_id: string; mention_required?: boolean }) {
    if (!connectivity.canMutate || busy) return
    setBusy(true)
    try {
      await saveWeixinBinding({
        id: 'binding-' + Date.now().toString(36),
        account_id: values.account_id,
        peer_id: values.peer_id || undefined,
        group_id: values.group_id || undefined,
        employee_id: values.employee_id,
        enabled: true,
        mention_required: Boolean(values.mention_required),
      })
      bindingForm.resetFields()
      await reload()
      actions.showToast({ messageKey: 'settings.weixinBindingSaved', tone: 'success' })
    } catch {
      actions.showToast({ messageKey: 'common.requestFailed', tone: 'error' })
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card className="projection-card weixin-channel-page" title={t('settings.weixinTitle', '微信连接')}>
      <Space direction="vertical" size="large" style={{ width: '100%' }}>
        <Alert type="info" showIcon message={t('settings.weixinExplicitStart', '微信消息只会创建排队中的 Employee Task；必须由 Owner 在 Tasks 中显式 Start。')} />
        <Space wrap>
          <Button type="primary" onClick={() => void addAccount()} disabled={!connectivity.canMutate || busy}>
            {t('settings.weixinAddAccount', '添加微信账号')}
          </Button>
          <Typography.Text type="secondary">{t('settings.weixinNoSecrets', '凭据只保存在服务端，浏览器不会保存或显示 Token。')}</Typography.Text>
        </Space>
        <List
          bordered
          dataSource={accounts}
          locale={{ emptyText: t('common.empty') }}
          renderItem={(account) => (
            <List.Item actions={[
              <Button key="logout" type="link" onClick={() => confirmLogout(account)} disabled={!connectivity.canMutate}>
                {t('settings.weixinLogout', '退出登录')}
              </Button>,
            ]}>
              <List.Item.Meta
                title={<Space>{account.label || account.id}<Tag color={stateColor[account.state]}>{t('settings.weixinState.' + account.state, account.state)}</Tag></Space>}
                description={account.id + (account.weixin_user_id ? ' · ' + account.weixin_user_id : '')}
              />
            </List.Item>
          )}
        />
        <Card size="small" title={t('settings.weixinBindings', 'Employee 绑定')}>
          <Form form={bindingForm} layout="vertical" onFinish={(values: { account_id: string; peer_id?: string; group_id?: string; employee_id: string; mention_required?: boolean }) => void submitBinding(values)}>
            <Form.Item name="account_id" label={t('settings.weixinAccount', '微信账号')} rules={[{ required: true }]}>
              <Select options={accountOptions} />
            </Form.Item>
            <Form.Item name="peer_id" label={t('settings.weixinPeer', 'Peer ID')}><Input /></Form.Item>
            <Form.Item name="group_id" label={t('settings.weixinGroup', 'Group ID')}><Input /></Form.Item>
            <Form.Item name="employee_id" label={t('settings.weixinEmployee', 'Employee')} rules={[{ required: true }]}>
              <Select options={employees.map((employee) => ({ value: employee.id, label: employee.name + ' (' + employee.id + ')' }))} />
            </Form.Item>
            <Button htmlType="submit" type="primary" disabled={!connectivity.canMutate || busy}>{t('actions.save')}</Button>
          </Form>
          <List
            size="small"
            dataSource={bindings}
            locale={{ emptyText: t('common.empty') }}
            renderItem={(binding) => (
              <List.Item actions={[
                <Button key="delete" danger type="link" onClick={() => void deleteWeixinBinding(binding.id).then(reload).catch(() => actions.showToast({ messageKey: 'common.requestFailed', tone: 'error' }))}>
                  {t('common.remove')}
                </Button>,
              ]}>
                <Typography.Text>{binding.account_id + ' · ' + (binding.peer_id || binding.group_id || '') + ' → ' + binding.employee_id}</Typography.Text>
              </List.Item>
            )}
          />
        </Card>
        <Card size="small" title={t('settings.weixinInbox', 'Weixin Inbox')}>
          <Space wrap style={{ width: '100%' }}>
            <Select
              aria-label={t('settings.weixinAccount', 'Weixin account')}
              placeholder={t('settings.weixinChooseAccount', 'Choose account')}
              value={inboxAccountID || undefined}
              options={accounts.map((account) => ({ value: account.id, label: account.label || account.id }))}
              onChange={(value) => {
                setInboxAccountID(value ?? '')
                setInbox([])
              }}
              style={{ minWidth: 220, maxWidth: '100%' }}
            />
            <Button onClick={() => void loadInbox()} disabled={!inboxAccountID || inboxBusy} loading={inboxBusy}>
              {t('settings.weixinLoadInbox', 'Load Inbox')}
            </Button>
          </Space>
          <List
            style={{ marginTop: 16 }}
            size="small"
            dataSource={inbox}
            locale={{ emptyText: t('settings.weixinInboxEmpty', 'No unbound messages') }}
            renderItem={(item) => (
              <List.Item>
                <List.Item.Meta
                  title={maskedPeer(item.group_id || item.peer_id) + ' · ' + item.state}
                  description={item.text ? item.text.slice(0, 160) : t('settings.weixinMessageMetadata', 'Message metadata only')}
                />
              </List.Item>
            )}
          />
        </Card>
      </Space>
      <Modal open={attempt !== null} title={t('settings.weixinQrTitle', '扫描微信二维码')} onCancel={() => void cancelLogin()} footer={[
        <Button key="cancel" onClick={() => void cancelLogin()}>{t('actions.cancel')}</Button>,
        <Button key="refresh" onClick={() => void refreshLogin()} disabled={busy}>{t('actions.retry')}</Button>,
      ]}>
        {attempt ? (
          <Space direction="vertical" align="center" style={{ width: '100%' }}>
            <img src={'/api/channels/weixin/login/' + encodeURIComponent(attempt.id) + '/qr'} alt={t('settings.weixinQrAlt', '微信登录二维码')} style={{ width: 'min(100%, 280px)', aspectRatio: '1', objectFit: 'contain' }} />
            <Tag color={stateColor[attempt.state]}>{t('settings.weixinState.' + attempt.state, attempt.state)}</Tag>
            <Typography.Text type="secondary">
              {t('settings.weixinExpiresIn', 'Expires in {{seconds}}s', { seconds: secondsRemaining })}
            </Typography.Text>
          </Space>
        ) : null}
      </Modal>
    </Card>
  )
}
