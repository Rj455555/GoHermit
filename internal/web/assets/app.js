const $ = selector => document.querySelector(selector);
let catalog = null;
let sessions = [];
let current = null;
let eventSource = null;
let lastSequence = 0;
let streamingBubble = null;
let loginTimer = null;
let connectionState = 'connecting';
let reconnecting = false;
let ownerProfile = null;
let approvals = [];
let approvalTimer = null;

async function request(url, options) {
  try {
    const response = await fetch(url, options);
    setConnectivity('online');
    const data = await response.json();
    if (!response.ok) {
      const error = new Error(data.error || '请求失败');
      error.status = response.status;
      throw error;
    }
    return data;
  } catch (error) {
    if (error instanceof TypeError) {
      setConnectivity('offline');
      throw new Error('无法连接 GoHermit 服务；Mac mini 可能离线或 SSH 隧道已断开');
    }
    throw error;
  }
}

function setConnectivity(state) {
  connectionState = state;
  const online = state === 'online';
  const reconnectingNow = state === 'reconnecting';
  const busy = online && catalog && catalog.active;
  $('#offline-banner').classList.toggle('hidden', online || reconnectingNow);
  $('#retry-connection').disabled = reconnectingNow;
  $('#service-status').textContent = online ? (busy ? 'Agent 运行中' : '服务正常') : (reconnectingNow ? '正在重新连接' : '连接异常');
  $('#service-dot').className = online ? (busy ? 'busy' : 'ready') : (reconnectingNow ? 'busy' : 'error');
  $('#health-dot').className = `health-dot ${online ? (busy ? 'busy' : 'ready') : (reconnectingNow ? 'busy' : 'error')}`;
  $('#health-dot').title = online ? (busy ? 'Agent 运行中' : '服务正常') : (reconnectingNow ? '正在重新连接' : '服务离线');
  $('#new-task').disabled = !online;
  $('#approve-plan').disabled = !online;
  for (const control of document.querySelectorAll('#new-task-options select')) control.disabled = !online;
  if (!online) {
    $('#task').disabled = true;
    $('#send').disabled = true;
    $('#composer-note').textContent = reconnectingNow ? '正在重新连接服务…' : '服务离线；输入内容不会发送，恢复连接后可继续。';
    setRunStatus(current ? '状态未知' : '离线', 'error');
  } else {
    $('#composer-note').textContent = '模型可能出错，请检查重要改动。';
    if (catalog) renderCatalog();
    if (current) renderRunState();
  }
}

async function checkHealth(manual = false) {
  if (reconnecting) return;
  reconnecting = true;
  const wasOffline = connectionState !== 'online';
  if (manual) setConnectivity('reconnecting');
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 4000);
  try {
    const response = await fetch('/api/health', {cache: 'no-store', signal: controller.signal});
    if (!response.ok) throw new Error('health check failed');
    setConnectivity('online');
    if (wasOffline) {
      await loadInfo();
      await loadSessions();
      toast('GoHermit 已重新连接');
    }
  } catch (_) {
    setConnectivity('offline');
  } finally {
    clearTimeout(timer);
    reconnecting = false;
  }
}

function toast(message, error = false) {
  const node = $('#toast');
  node.textContent = message;
  node.className = error ? 'show error' : 'show';
  clearTimeout(node.timer);
  node.timer = setTimeout(() => { node.className = ''; }, 3500);
}

function setOptions(select, items, selected, label) {
  select.replaceChildren();
  for (const item of items || []) {
    const option = document.createElement('option');
    option.value = item.id;
    option.textContent = label(item);
    select.append(option);
  }
  if ((items || []).some(item => item.id === selected)) select.value = selected;
}

function selectedCompany() {
  return (catalog.available_companies || []).find(item => item.id === $('#company').value);
}

function selectedAccess() {
  const company = selectedCompany();
  return company && company.access.find(item => item.id === $('#access').value);
}

function selectedAgent() {
  return (catalog.agents || []).find(item => item.id === $('#agent').value);
}

function fillModels(preferred) {
  const access = selectedAccess();
  setOptions($('#model'), access ? access.models : [], preferred, item => item.label);
  renderSelectionHint();
}

function fillAccess(preferredAccess, preferredModel) {
  const company = selectedCompany();
  setOptions($('#access'), company ? company.access : [], preferredAccess, item => item.label);
  fillModels(preferredModel);
}

function renderCatalog() {
  const available = catalog.available_companies || [];
  const selection = catalog.selection || {};
  setOptions($('#company'), available, selection.company, item => item.label);
  setOptions($('#agent'), catalog.agents || [], selection.agent, item => item.label);
  fillAccess(selection.access, selection.model);
  const enabled = connectionState === 'online' && available.length > 0;
  $('#task').disabled = !enabled;
  $('#send').disabled = !enabled;
  $('#task').placeholder = enabled ? '描述任务，或继续当前会话…' : '请先在设置中接入一个模型';
}

function renderSelectionHint() {
  if (!catalog || current || connectionState !== 'online') return;
  const company = selectedCompany();
  const access = selectedAccess();
  const agent = selectedAgent();
  const model = access && access.models.find(item => item.id === $('#model').value);
  if (company && access && agent && model) {
    $('#thread-meta').textContent = `${model.label} · ${agent.label}`;
    $('#send').disabled = false;
  } else {
    $('#thread-meta').textContent = '请先在设置中接入一个可用模型';
    $('#send').disabled = true;
  }
}

async function loadInfo() {
  catalog = await request('/api/info');
  $('#version').textContent = `v${catalog.version}`;
  $('#workspace').textContent = catalog.workspace;
  $('#workspace').title = catalog.workspace;
  $('#service-status').textContent = catalog.active ? 'Agent 运行中' : '服务正常';
  $('#service-dot').className = catalog.active ? 'busy' : 'ready';
  $('#health-dot').className = `health-dot ${catalog.active ? 'busy' : 'ready'}`;
  $('#health-dot').title = catalog.active ? 'Agent 运行中' : '服务正常';
  renderCatalog();
  renderSettings();
}

async function loadSessions(openSelected = true) {
  const data = await request('/api/sessions?limit=100');
  sessions = data.sessions || [];
  $('#session-count').textContent = String(sessions.length);
  renderSessionList();
  if (!openSelected) return;
  const selected = localStorage.getItem('gohermit.session');
  if (selected && sessions.some(item => item.id === selected)) await openSession(selected);
  else showNewTask(false);
}

function renderSessionList() {
  const root = $('#session-list');
  root.replaceChildren();
  if (!sessions.length) {
    const empty = document.createElement('div');
    empty.className = 'sidebar-empty';
    empty.textContent = '任务会显示在这里';
    root.append(empty);
    return;
  }
  for (const item of sessions) {
    const button = document.createElement('button');
    button.className = `session-item ${current && current.session.id === item.id ? 'active' : ''}`;
    button.innerHTML = '<span class="session-state"></span><span class="session-copy"><strong></strong><small></small></span>';
    button.querySelector('.session-state').classList.add(statusClass(item.last_run_status));
    button.querySelector('strong').textContent = item.title;
    button.querySelector('small').textContent = `${statusText(item.last_run_status)} · ${relativeTime(item.updated_at)}`;
    button.addEventListener('click', () => openSession(item.id));
    root.append(button);
  }
}

function relativeTime(value) {
  const time = new Date(value).getTime();
  const seconds = Math.max(0, Math.floor((Date.now() - time) / 1000));
  if (seconds < 60) return '刚刚';
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟前`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)} 小时前`;
  return new Date(value).toLocaleDateString();
}

function statusText(status) {
  return ({queued: '等待中', running: '运行中', verifying: '验证中', completed: '已完成', failed: '失败', cancelled: '已停止', interrupted: '可恢复'})[status] || '就绪';
}

function statusClass(status) {
  if (status === 'completed') return 'success';
  if (status === 'failed' || status === 'cancelled') return 'error';
  if (status === 'running' || status === 'queued' || status === 'verifying') return 'running';
  if (status === 'interrupted') return 'warning';
  return 'idle';
}

function showNewTask(focus = true) {
  closeEvents();
  current = null;
  localStorage.removeItem('gohermit.session');
  $('#thread-title').textContent = '新任务';
  $('#welcome').classList.remove('hidden');
  $('#thread').classList.add('empty-thread');
  $('#messages').replaceChildren();
  approvals = [];
  renderApprovals();
  $('#plan-panel').classList.add('hidden');
  $('#team-panel').classList.add('hidden');
  resetActivity();
  $('#new-task-options').classList.remove('hidden');
  $('#session-model').classList.add('hidden');
  $('#approve-plan').classList.add('hidden');
  $('#resume-run').classList.add('hidden');
  $('#cancel-run').classList.add('hidden');
  setRunStatus('就绪', 'idle');
  renderSelectionHint();
  renderSessionList();
  closeMobileSidebar();
  if (focus && !$('#task').disabled) $('#task').focus();
}

async function createSession() {
  const payload = {
    company: $('#company').value,
    access: $('#access').value,
    model: $('#model').value,
    agent: $('#agent').value,
    plan_mode: $('#plan-mode').value
  };
  const session = await request('/api/sessions', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify(payload)
  });
  await loadSessions(false);
  await openSession(session.id);
  return session.id;
}

async function openSession(id) {
  closeEvents();
  current = await request(`/api/sessions/${id}`);
  localStorage.setItem('gohermit.session', id);
  $('#welcome').classList.add('hidden');
  $('#thread').classList.remove('empty-thread');
  $('#new-task-options').classList.add('hidden');
  $('#session-model').classList.remove('hidden');
  renderThreadIdentity();
  renderMessages(current.messages || []);
  renderPlan();
  renderMission();
  resetActivity();
  renderRunState();
  await loadApprovals();
  lastSequence = 0;
  connectEvents(id);
  renderSessionList();
  closeMobileSidebar();
}

function renderThreadIdentity() {
  if (!current) return;
  const selection = current.session.selection || {};
  $('#thread-title').textContent = current.session.title;
  $('#thread-meta').textContent = [selection.model, agentLabel(selection.agent)].filter(Boolean).join(' · ');
  $('#session-model').textContent = [selection.model, agentLabel(selection.agent)].filter(Boolean).join(' · ');
}

function agentLabel(id) {
  const agent = catalog && (catalog.agents || []).find(item => item.id === id);
  return agent ? agent.label : id;
}

function renderMessages(messages) {
  const root = $('#messages');
  root.replaceChildren();
  for (const message of messages) addMessage(message.role, message.content, false);
  requestAnimationFrame(() => { $('#thread').scrollTop = $('#thread').scrollHeight; });
}

function renderPlan() {
  const panel = $('#plan-panel');
  const run = activeRun() || lastRun();
  const plan = run && run.plan;
  if (!plan || !(plan.steps || []).length) {
    panel.classList.add('hidden');
    return;
  }
  panel.classList.remove('hidden');
  const steps = plan.steps || [];
  const done = steps.filter(step => step.status === 'completed').length;
  const currentStep = steps.find(step => step.status === 'in_progress');
  const failedStep = steps.find(step => step.status === 'failed');
  const cancelledStep = steps.find(step => step.status === 'cancelled');
  $('#plan-current').textContent = currentStep ? `正在进行：${currentStep.title}` : (failedStep ? `失败：${failedStep.title}` : (cancelledStep ? '计划已停止' : (plan.status === 'completed' ? '全部完成' : '等待下一步')));
  $('#plan-progress-text').textContent = `${done} / ${steps.length}`;
  $('#plan-progress-bar').style.width = `${steps.length ? Math.round(done * 100 / steps.length) : 0}%`;
  const root = $('#plan-steps');
  root.replaceChildren();
  for (const step of steps) {
    const row = document.createElement('li');
    row.className = `plan-step ${step.status}`;
    row.innerHTML = '<span class="plan-check" aria-hidden="true"></span><div><strong></strong><span></span></div>';
    row.querySelector('.plan-check').textContent = ({completed: '✓', in_progress: '•', failed: '!', cancelled: '–'})[step.status] || '';
    row.querySelector('strong').textContent = step.title;
    const detail = row.querySelector('div span');
    detail.textContent = step.detail || ({pending: '等待处理', in_progress: '正在处理', completed: '已完成', failed: '未完成', cancelled: '已停止'})[step.status] || step.status;
    root.append(row);
  }
}

function roleLabel(role) {
  return ({lead: 'Lead', explorer: 'Explorer', builder: 'Builder', reviewer: 'Reviewer', verifier: 'Verifier', operator: 'Operator'})[role] || role;
}

function renderMission() {
  const panel = $('#team-panel');
  const mission = current && current.session.mission;
  if (!mission) {
    panel.classList.add('hidden');
    return;
  }
  panel.classList.remove('hidden');
  $('#team-summary').textContent = ({queued: '等待开始', running: '团队正在协作', completed: '已完成', failed: '执行失败', interrupted: '可恢复'})[mission.status] || mission.status;
  const usage = mission.usage || {};
  const budget = mission.budget || {};
  $('#team-budget').textContent = `${usage.model_calls || 0}/${budget.max_model_calls || '—'} 次模型调用`;
  const root = $('#team-members');
  root.replaceChildren();
  for (const item of mission.work_items || []) {
    const card = document.createElement('div');
    card.className = `team-member ${item.status || 'queued'}`;
    card.dataset.workItem = item.id;
    card.innerHTML = '<i></i><div><strong></strong><span></span></div>';
    card.querySelector('strong').textContent = roleLabel(item.role);
    card.querySelector('span').textContent = `${item.title} · ${statusText(item.status)}`;
    root.append(card);
  }
}

function renderMissionEvent(type, runtimeEvent) {
  if (!type.startsWith('work_item_') && !type.startsWith('mission_')) return;
  if (type.startsWith('mission_')) {
    $('#team-summary').textContent = eventLabel(type);
    return;
  }
  const card = [...document.querySelectorAll('.team-member')].find(node => node.dataset.workItem === runtimeEvent.work_item_id);
  if (!card) return;
  const state = type === 'work_item_started' ? 'running' : (type === 'work_item_completed' ? 'completed' : 'failed');
  card.className = `team-member ${state}`;
  const detail = card.querySelector('span');
  if (detail) detail.textContent = `${runtimeEvent.message || runtimeEvent.work_item_id} · ${statusText(state)}`;
}

function addMessage(role, content, scroll = true) {
  const root = $('#messages');
  const row = document.createElement('article');
  row.className = `message ${role}`;
  const avatar = document.createElement('div');
  avatar.className = 'message-avatar';
  avatar.textContent = role === 'user' ? '你' : 'G';
  const body = document.createElement('div');
  body.className = 'message-content';
  body.textContent = content;
  row.append(avatar, body);
  root.append(row);
  if (scroll) requestAnimationFrame(() => { $('#thread').scrollTop = $('#thread').scrollHeight; });
  return body;
}

function activeRun() {
  if (!current || !current.session.active_run_id) return null;
  return (current.session.runs || []).find(run => run.id === current.session.active_run_id);
}

function lastRun() {
  if (!current || !(current.session.runs || []).length) return null;
  return current.session.runs[current.session.runs.length - 1];
}

function renderRunState() {
  const run = activeRun();
  const latest = run || lastRun();
  const status = latest && latest.status;
  setRunStatus(statusText(status), statusClass(status));
	const awaitingApproval = Boolean(run && run.status === 'queued' && run.plan_mode === 'review' && !run.plan_approved);
  const busy = run && ['queued', 'running', 'verifying'].includes(run.status) && !awaitingApproval;
	const occupied = Boolean(busy || awaitingApproval);
  $('#send').disabled = occupied;
	$('#approve-plan').classList.toggle('hidden', !awaitingApproval);
  $('#cancel-run').classList.toggle('hidden', !(busy || awaitingApproval));
  $('#resume-run').classList.toggle('hidden', !(run && run.status === 'interrupted'));
	$('#task').disabled = occupied;
  $('#task').placeholder = busy ? 'GoHermit 正在处理当前任务…' : '继续当前任务…';
}

function setRunStatus(text, state) {
  $('#run-status').textContent = text;
  $('#run-status').className = `run-status ${state || ''}`;
}

async function sendMessage() {
  const message = $('#task').value.trim();
  if (!message) { $('#task').focus(); return; }
  try {
    if (!current) await createSession();
    const id = current.session.id;
    connectEvents(id);
    $('#send').disabled = true;
    $('#task').disabled = true;
    $('#welcome').classList.add('hidden');
    $('#thread').classList.remove('empty-thread');
    addMessage('user', message);
    $('#task').value = '';
    const result = await request(`/api/sessions/${id}/runs`, {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({message})
    });
    current.session.active_run_id = result.run_id;
    current.session.runs = current.session.runs || [];
    current.session.runs.push({id: result.run_id, status: 'queued', message});
    renderRunState();
    current = await request(`/api/sessions/${id}`);
    renderPlan();
    renderMission();
    await loadInfo();
  } catch (error) {
    toast(error.message, true);
    if (current) await refreshCurrent();
    else renderSelectionHint();
  }
}

function connectEvents(sessionID) {
  if (eventSource && eventSource.sessionID === sessionID) return;
  closeEvents();
  eventSource = new EventSource(`/api/sessions/${sessionID}/events?after=${lastSequence}`);
  eventSource.sessionID = sessionID;
  const types = ['task_started', 'turn_started', 'model_started', 'model_delta', 'model_completed', 'tool_started', 'tool_completed', 'permission_required', 'checkpoint_saved', 'run_verifying', 'run_interrupted', 'workspace_changed', 'memory_updated', 'session_updated', 'plan_created', 'plan_updated', 'mission_started', 'mission_completed', 'mission_failed', 'work_item_started', 'work_item_completed', 'work_item_failed', 'approval_requested', 'approval_decided', 'approval_expired', 'approval_consumed', 'task_completed', 'task_failed', 'task_cancelled'];
  for (const type of types) eventSource.addEventListener(type, source => consumeEvent(type, source));
  eventSource.onerror = () => {
    if (current && current.session.id === sessionID) $('#composer-note').textContent = '事件连接正在重试…';
    checkHealth();
  };
}

function closeEvents() {
  if (eventSource) eventSource.close();
  eventSource = null;
  streamingBubble = null;
}

function consumeEvent(type, sourceEvent) {
  let runtimeEvent;
  try { runtimeEvent = JSON.parse(sourceEvent.data); } catch (_) { return; }
  if (runtimeEvent.sequence) lastSequence = Math.max(lastSequence, runtimeEvent.sequence);
  if (type === 'model_delta') {
    if (!streamingBubble) streamingBubble = addMessage('assistant', '');
    streamingBubble.textContent += runtimeEvent.message || '';
    $('#thread').scrollTop = $('#thread').scrollHeight;
    return;
  }
  if (type === 'model_started') streamingBubble = null;
  const agent = runtimeEvent.agent_id ? `${roleLabel(runtimeEvent.agent_id)} · ` : '';
  addActivity(type, agent + (runtimeEvent.error || runtimeEvent.message || runtimeEvent.tool || ''));
  if ((type === 'plan_created' || type === 'plan_updated') && runtimeEvent.data && runtimeEvent.data.plan && current) {
    const run = (current.session.runs || []).find(item => item.id === runtimeEvent.run_id);
    if (run) run.plan = runtimeEvent.data.plan;
    renderPlan();
  }
  renderMissionEvent(type, runtimeEvent);
  if (type === 'approval_requested') loadApprovals();
  if (['approval_decided', 'approval_expired', 'approval_consumed'].includes(type)) {
    const requestID = runtimeEvent.data && runtimeEvent.data.request_id;
    if (requestID && approvals.some(item => item.request_id === requestID)) {
      approvals = approvals.filter(item => item.request_id !== requestID);
      renderApprovals();
    }
  }
  if (type === 'task_started') setBusyState('运行中', 'running');
  if (type === 'run_verifying') setBusyState('验证中', 'running');
  if (['task_completed', 'task_failed', 'task_cancelled'].includes(type)) {
    streamingBubble = null;
    setTimeout(refreshCurrent, 120);
  }
}

function resetActivity() {
  $('#events').replaceChildren();
  $('#activity-count').textContent = '0';
  $('#activity-panel').classList.add('hidden');
  $('#activity-panel').open = false;
}

function addActivity(type, message) {
  $('#activity-panel').classList.remove('hidden');
  const root = $('#events');
  const row = document.createElement('div');
  row.className = `event ${type.includes('failed') || type.includes('cancelled') ? 'error' : ''}`;
  const icon = document.createElement('span');
  icon.className = 'event-icon';
  icon.textContent = eventIcon(type);
  const copy = document.createElement('div');
  const title = document.createElement('strong');
  title.textContent = eventLabel(type);
  const detail = document.createElement('span');
  detail.textContent = message || '—';
  copy.append(title, detail);
  row.append(icon, copy);
  root.append(row);
  $('#activity-count').textContent = String(root.children.length);
}

function eventIcon(type) {
  if (type.includes('tool')) return '⌘';
  if (type.includes('failed') || type.includes('cancelled') || type.includes('expired')) return '!';
  if (type.includes('approval')) return '✋';
  if (type.includes('verif')) return '✓';
  if (type.includes('memory') || type.includes('checkpoint')) return '◇';
  return '·';
}

function eventLabel(type) {
  return ({task_started: '开始运行', turn_started: '新一轮', model_started: '模型处理中', model_completed: '模型响应完成', tool_started: '调用工具', tool_completed: '工具完成', permission_required: '需要权限', checkpoint_saved: '已保存状态', run_verifying: '验证改动', run_interrupted: '运行中断', workspace_changed: '工作区已变化', memory_updated: '项目记忆已更新', plan_created: '执行计划已创建', plan_updated: '执行计划已更新', mission_started: '团队任务开始', mission_completed: '团队任务完成', mission_failed: '团队任务失败', work_item_started: 'Agent 开始工作', work_item_completed: 'Agent 完成交接', work_item_failed: 'Agent 执行失败', approval_requested: '操作等待批准', approval_decided: '审批已决定', approval_expired: '审批请求已过期', approval_consumed: '审批已执行', task_completed: '任务完成', task_failed: '任务失败', task_cancelled: '任务已停止'})[type] || type;
}

function setBusyState(text, state) {
  setRunStatus(text, state);
  $('#send').disabled = true;
  $('#task').disabled = true;
  $('#cancel-run').classList.remove('hidden');
}

async function refreshCurrent() {
  if (!current) return;
  const id = current.session.id;
  current = await request(`/api/sessions/${id}`);
  renderThreadIdentity();
  renderMessages(current.messages || []);
  renderPlan();
  renderMission();
  renderRunState();
  await loadApprovals();
  $('#composer-note').textContent = '模型可能出错，请检查重要改动。';
  await loadSessions(false);
  await loadInfo();
}

async function cancelRun() {
  const run = activeRun();
  if (!run) return;
  try {
    await request(`/api/sessions/${current.session.id}/runs/${run.id}/cancel`, {method: 'POST'});
    setRunStatus('正在停止', 'warning');
  } catch (error) { toast(error.message, true); }
}

async function approvePlan() {
  const run = activeRun();
  if (!run) return;
  try {
    $('#approve-plan').disabled = true;
    await request(`/api/sessions/${current.session.id}/runs/${run.id}/approve`, {method: 'POST'});
    await refreshCurrent();
  } catch (error) {
    toast(error.message, true);
  } finally {
    $('#approve-plan').disabled = false;
  }
}

async function resumeRun() {
  const run = activeRun();
  if (!run) return;
  try {
    connectEvents(current.session.id);
    await request(`/api/sessions/${current.session.id}/runs/${run.id}/resume`, {method: 'POST'});
    setBusyState('恢复中', 'running');
  } catch (error) { toast(error.message, true); }
}

async function loadApprovals() {
  if (!current) { approvals = []; renderApprovals(); return; }
  try {
    const data = await request(`/api/sessions/${current.session.id}/approvals?status=pending`);
    approvals = data.approvals || [];
  } catch (_) {
    approvals = [];
  }
  renderApprovals();
}

function renderApprovals() {
  const panel = $('#approval-panel');
  const root = $('#approval-list');
  root.replaceChildren();
  if (!current || !approvals.length) {
    panel.classList.add('hidden');
    clearInterval(approvalTimer);
    approvalTimer = null;
    return;
  }
  panel.classList.remove('hidden');
  $('#approval-heading').textContent = `${approvals.length} 个操作等待你的批准，过期将自动拒绝`;
  for (const item of approvals) root.append(approvalCard(item));
  updateApprovalCountdowns();
  if (!approvalTimer) approvalTimer = setInterval(updateApprovalCountdowns, 1000);
}

function approvalCard(item) {
  const card = document.createElement('article');
  card.className = 'approval-card';
  card.dataset.requestId = item.request_id;
  card.innerHTML = '<div class="approval-copy"><strong></strong><span class="approval-paths"></span><span class="approval-args"></span></div><div class="approval-side"><span class="approval-countdown"></span><div class="approval-actions"><button class="small-button primary approval-approve">批准</button><button class="small-button danger-text approval-deny">拒绝</button></div></div>';
  card.querySelector('strong').textContent = item.tool;
  card.querySelector('.approval-paths').textContent = (item.resource_paths || []).join('、');
  const args = card.querySelector('.approval-args');
  args.textContent = item.args_summary || '';
  args.classList.toggle('hidden', !item.args_summary);
  card.querySelector('.approval-approve').addEventListener('click', () => decideApproval(item.request_id, 'approve', card));
  card.querySelector('.approval-deny').addEventListener('click', () => decideApproval(item.request_id, 'deny', card));
  return card;
}

async function decideApproval(requestID, decision, card) {
  const buttons = card.querySelectorAll('button');
  for (const button of buttons) button.disabled = true;
  try {
    await request(`/api/sessions/${current.session.id}/approvals/${requestID}/decide`, {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({decision})
    });
    approvals = approvals.filter(item => item.request_id !== requestID);
    renderApprovals();
    toast(decision === 'approve' ? '已批准该操作' : '已拒绝该操作');
  } catch (error) {
    if (error.status === 409) {
      toast('该请求已被处理或已过期', true);
      await loadApprovals();
    } else {
      toast(error.message, true);
      updateApprovalCountdowns();
      for (const button of buttons) button.disabled = card.classList.contains('expired');
    }
  }
}

function updateApprovalCountdowns() {
  const now = Date.now();
  for (const card of document.querySelectorAll('.approval-card')) {
    const item = approvals.find(entry => entry.request_id === card.dataset.requestId);
    if (!item) continue;
    const countdown = card.querySelector('.approval-countdown');
    const remaining = new Date(item.expires_at).getTime() - now;
    if (remaining <= 0) {
      card.classList.add('expired');
      countdown.textContent = '已过期，等待确认';
      for (const button of card.querySelectorAll('button')) button.disabled = true;
      continue;
    }
    const seconds = Math.floor(remaining / 1000);
    countdown.textContent = `剩余 ${Math.floor(seconds / 60)} 分 ${String(seconds % 60).padStart(2, '0')} 秒`;
  }
}

async function openSettings() {
  $('#settings-drawer').classList.remove('hidden');
  $('#drawer-backdrop').classList.remove('hidden');
  $('#settings-button').classList.add('active');
  try {
    ownerProfile = await request('/api/owner');
    renderOwner();
  } catch (error) { toast(error.message, true); }
}

function closeSettings() {
  $('#settings-drawer').classList.add('hidden');
  $('#drawer-backdrop').classList.add('hidden');
  $('#settings-button').classList.remove('active');
}

function renderSettings() {
  const root = $('#settings-list');
  root.replaceChildren();
  for (const company of catalog.companies || []) {
    const group = document.createElement('section');
    group.className = 'provider-group';
    const heading = document.createElement('h3');
    heading.textContent = company.label;
    group.append(heading);
    for (const access of company.access) group.append(accessCard(company, access));
    root.append(group);
  }
}

function renderOwner() {
  if (!ownerProfile) return;
  const identity = ownerProfile.identity || {};
  const preferences = ownerProfile.preferences || {};
  $('#owner-name').value = identity.display_name || '';
  $('#owner-language').value = identity.language || '';
  $('#owner-timezone').value = identity.timezone || '';
  $('#owner-communication').value = preferences.communication || '';
  $('#owner-coding').value = preferences.coding || '';
  $('#owner-verification').value = preferences.verification || '';
  const root = $('#owner-facts');
  root.replaceChildren();
  for (const fact of ownerProfile.facts || []) {
    const card = document.createElement('div');
    card.className = 'owner-fact';
    card.innerHTML = '<div><strong></strong><span></span></div><button>忘记</button>';
    card.querySelector('strong').textContent = `${fact.category}${fact.confirmed ? ' · 已确认' : ''}`;
    card.querySelector('span').textContent = fact.value;
    card.querySelector('button').addEventListener('click', () => forgetFact(fact.id));
    root.append(card);
  }
  if (!root.children.length) {
    const empty = document.createElement('div');
    empty.className = 'owner-empty';
    empty.textContent = '还没有长期记忆。只保存你明确确认的事实。';
    root.append(empty);
  }
}

async function saveOwner() {
  const existing = ownerProfile || {};
  const payload = {
    schema_version: existing.schema_version || 1,
    identity: {display_name: $('#owner-name').value.trim(), language: $('#owner-language').value.trim(), timezone: $('#owner-timezone').value.trim()},
    preferences: {...(existing.preferences || {}), communication: $('#owner-communication').value.trim(), coding: $('#owner-coding').value.trim(), verification: $('#owner-verification').value.trim()},
    environments: existing.environments || [], facts: existing.facts || []
  };
  try {
    ownerProfile = await request('/api/owner', {method: 'PUT', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(payload)});
    renderOwner();
    toast('个人配置已保存');
  } catch (error) { toast(error.message, true); }
}

async function addOwnerFact() {
  const category = $('#fact-category').value.trim();
  const value = $('#fact-value').value.trim();
  if (!category || !value) { toast('请填写分类和记忆内容', true); return; }
  const id = `fact-${Date.now()}`;
  try {
    ownerProfile = await request(`/api/owner/facts/${id}`, {method: 'PUT', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({category, value, source: 'owner-settings', confirmed: true})});
    $('#fact-category').value = '';
    $('#fact-value').value = '';
    renderOwner();
    toast('长期记忆已添加');
  } catch (error) { toast(error.message, true); }
}

async function forgetFact(id) {
  try {
    ownerProfile = await request(`/api/owner/facts/${id}`, {method: 'DELETE'});
    renderOwner();
    toast('长期记忆已删除');
  } catch (error) { toast(error.message, true); }
}

function accessCard(company, access) {
  const status = catalog.auth_status[access.id] || {configured: false, detail: '尚未配置'};
  const card = document.createElement('article');
  card.className = 'access-card';
  const top = document.createElement('div');
  top.className = 'access-top';
  top.innerHTML = '<span class="provider-logo"></span><div class="access-copy"><strong></strong><small></small></div><span class="connection-state"></span>';
  top.querySelector('.provider-logo').textContent = company.label.slice(0, 1);
  top.querySelector('.access-copy strong').textContent = access.label;
  top.querySelector('.access-copy small').textContent = status.configured ? `${status.detail} · ${status.source}` : status.detail;
  const badge = top.querySelector('.connection-state');
  badge.className = `connection-state ${status.configured ? 'connected' : ''}`;
  badge.textContent = status.configured ? '已连接' : '未连接';
  card.append(top);
  const controls = document.createElement('div');
  controls.className = 'access-controls';
  if (access.auth_type === 'api_key') {
    controls.innerHTML = '<input type="password" autocomplete="off" placeholder="粘贴 API Key"><button class="small-button">保存</button>';
    controls.querySelector('button').addEventListener('click', () => saveKey(access.id, controls.querySelector('input')));
    if (status.configured && status.source === 'GoHermit 设置') controls.append(deleteButton(access.id, '移除'));
  } else if (status.configured) {
    const text = document.createElement('span');
    text.className = 'connected-copy';
    text.textContent = 'Codex 登录有效';
    controls.append(text);
    if ((status.source || '').includes('GoHermit')) controls.append(deleteButton(access.id, '退出'));
  } else {
    const button = document.createElement('button');
    button.className = 'small-button primary';
    button.textContent = '登录 Codex';
    button.addEventListener('click', () => startCodexLogin(button, card));
    controls.append(button);
  }
  card.append(controls);
  return card;
}

function deleteButton(provider, label) {
  const button = document.createElement('button');
  button.className = 'small-button danger-text';
  button.textContent = label;
  button.addEventListener('click', () => deleteCredential(provider));
  return button;
}

async function saveKey(provider, input) {
  const key = input.value.trim();
  if (!key) { input.focus(); return; }
  try {
    await request(`/api/settings/providers/${provider}/api-key`, {method: 'PUT', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({api_key: key})});
    input.value = '';
    toast('API Key 已保存');
    await loadInfo();
  } catch (error) { toast(error.message, true); }
}

async function deleteCredential(provider) {
  try {
    await request(`/api/settings/providers/${provider}/credentials`, {method: 'DELETE'});
    toast('本地凭据已移除');
    await loadInfo();
  } catch (error) { toast(error.message, true); }
}

async function startCodexLogin(button, card) {
  button.disabled = true;
  button.textContent = '正在准备…';
  try {
    const login = await request('/api/settings/providers/openai-codex/login', {method: 'POST'});
    const box = document.createElement('div');
    box.className = 'device-code';
    const text = document.createElement('span');
    text.textContent = '打开登录页面并输入代码';
    const code = document.createElement('strong');
    code.textContent = login.user_code;
    const link = document.createElement('a');
    link.href = login.verification_url;
    link.target = '_blank';
    link.rel = 'noopener noreferrer';
    link.textContent = '打开 OpenAI 登录页面 →';
    box.append(text, code, link);
    card.append(box);
    pollLogin(login.id);
  } catch (error) {
    button.disabled = false;
    button.textContent = '登录 Codex';
    toast(error.message, true);
  }
}

function pollLogin(id) {
  clearTimeout(loginTimer);
  loginTimer = setTimeout(async () => {
    try {
      const login = await request(`/api/settings/logins/${id}`);
      if (login.status === 'approved') { toast('Codex 登录成功'); await loadInfo(); return; }
      if (login.status === 'error' || login.status === 'expired') { toast(login.error || 'Codex 登录失败', true); await loadInfo(); return; }
      pollLogin(id);
    } catch (error) { toast(error.message, true); }
  }, 2000);
}

function openMobileSidebar() { $('#app').classList.add('sidebar-open'); }
function closeMobileSidebar() { $('#app').classList.remove('sidebar-open'); }

$('#company').addEventListener('change', () => fillAccess('', ''));
$('#access').addEventListener('change', () => fillModels(''));
$('#model').addEventListener('change', renderSelectionHint);
$('#agent').addEventListener('change', renderSelectionHint);
$('#new-task').addEventListener('click', () => showNewTask());
$('#brand-button').addEventListener('click', () => showNewTask());
$('#tasks-button').addEventListener('click', openMobileSidebar);
$('#send').addEventListener('click', sendMessage);
$('#cancel-run').addEventListener('click', cancelRun);
$('#approve-plan').addEventListener('click', approvePlan);
$('#resume-run').addEventListener('click', resumeRun);
$('#task').addEventListener('keydown', event => { if ((event.ctrlKey || event.metaKey) && event.key === 'Enter') sendMessage(); });
$('#settings-button').addEventListener('click', openSettings);
$('#settings-close').addEventListener('click', closeSettings);
$('#drawer-backdrop').addEventListener('click', closeSettings);
$('#sidebar-open').addEventListener('click', openMobileSidebar);
$('#sidebar-close').addEventListener('click', closeMobileSidebar);
$('#retry-connection').addEventListener('click', () => checkHealth(true));
$('#save-owner').addEventListener('click', saveOwner);
$('#add-fact').addEventListener('click', addOwnerFact);
document.addEventListener('keydown', event => {
  if (event.key !== 'Escape') return;
  closeSettings();
  closeMobileSidebar();
});

(async function boot() {
  try {
    await loadInfo();
    await loadSessions();
  } catch (error) {
    setConnectivity('offline');
    toast(error.message, true);
  }
  setInterval(checkHealth, 5000);
})();
