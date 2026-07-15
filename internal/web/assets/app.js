const $ = selector => document.querySelector(selector);
let catalog = null;
let sessions = [];
let current = null;
let eventSource = null;
let lastSequence = 0;
let streamingBubble = null;
let loginTimer = null;

async function request(url, options) {
  const response = await fetch(url, options);
  const data = await response.json();
  if (!response.ok) throw new Error(data.error || '请求失败');
  return data;
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
  const enabled = available.length > 0;
  $('#task').disabled = !enabled;
  $('#send').disabled = !enabled;
  $('#task').placeholder = enabled ? '描述任务，或继续当前会话…' : '请先在设置中接入一个模型';
}

function renderSelectionHint() {
  if (!catalog || current) return;
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
  resetActivity();
  $('#new-task-options').classList.remove('hidden');
  $('#session-model').classList.add('hidden');
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
    agent: $('#agent').value
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
  resetActivity();
  renderRunState();
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
  const busy = run && ['queued', 'running', 'verifying'].includes(run.status);
  $('#send').disabled = Boolean(busy);
  $('#cancel-run').classList.toggle('hidden', !busy);
  $('#resume-run').classList.toggle('hidden', !(run && run.status === 'interrupted'));
  $('#task').disabled = Boolean(busy);
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
  const types = ['task_started', 'turn_started', 'model_started', 'model_delta', 'model_completed', 'tool_started', 'tool_completed', 'permission_required', 'checkpoint_saved', 'run_verifying', 'run_interrupted', 'workspace_changed', 'memory_updated', 'session_updated', 'task_completed', 'task_failed', 'task_cancelled'];
  for (const type of types) eventSource.addEventListener(type, source => consumeEvent(type, source));
  eventSource.onerror = () => {
    if (current && current.session.id === sessionID) $('#composer-note').textContent = '事件连接正在重试…';
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
  addActivity(type, runtimeEvent.error || runtimeEvent.message || runtimeEvent.tool || '');
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
  if (type.includes('failed') || type.includes('cancelled')) return '!';
  if (type.includes('verif')) return '✓';
  if (type.includes('memory') || type.includes('checkpoint')) return '◇';
  return '·';
}

function eventLabel(type) {
  return ({task_started: '开始运行', turn_started: '新一轮', model_started: '模型处理中', model_completed: '模型响应完成', tool_started: '调用工具', tool_completed: '工具完成', permission_required: '需要权限', checkpoint_saved: '已保存状态', run_verifying: '验证改动', run_interrupted: '运行中断', workspace_changed: '工作区已变化', memory_updated: '项目记忆已更新', task_completed: '任务完成', task_failed: '任务失败', task_cancelled: '任务已停止'})[type] || type;
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
  renderRunState();
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

async function resumeRun() {
  const run = activeRun();
  if (!run) return;
  try {
    connectEvents(current.session.id);
    await request(`/api/sessions/${current.session.id}/runs/${run.id}/resume`, {method: 'POST'});
    setBusyState('恢复中', 'running');
  } catch (error) { toast(error.message, true); }
}

function openSettings() {
  $('#settings-drawer').classList.remove('hidden');
  $('#drawer-backdrop').classList.remove('hidden');
  $('#settings-button').classList.add('active');
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
$('#resume-run').addEventListener('click', resumeRun);
$('#task').addEventListener('keydown', event => { if ((event.ctrlKey || event.metaKey) && event.key === 'Enter') sendMessage(); });
$('#settings-button').addEventListener('click', openSettings);
$('#settings-close').addEventListener('click', closeSettings);
$('#drawer-backdrop').addEventListener('click', closeSettings);
$('#sidebar-open').addEventListener('click', openMobileSidebar);
$('#sidebar-close').addEventListener('click', closeMobileSidebar);
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
    $('#service-status').textContent = '连接异常';
    $('#service-dot').className = 'error';
    toast(error.message, true);
  }
})();
