const $ = selector => document.querySelector(selector);
let catalog = null;
let sessions = [];
let current = null;
let eventSource = null;
let lastSequence = 0;
let streamingBubble = null;
let loginTimer = null;

const titles = {
  dashboard: ['概览', '查看工作区、会话和模型接入状态'],
  agent: ['Agent', '持续对话、恢复任务并查看验证结果'],
  settings: ['模型接入', '管理登录和 API Key']
};

function showPage(name) {
  document.querySelectorAll('.page').forEach(node => node.classList.toggle('active', node.id === `page-${name}`));
  document.querySelectorAll('.nav-item').forEach(node => node.classList.toggle('active', node.dataset.page === name));
  $('#page-title').textContent = titles[name][0];
  $('#page-subtitle').textContent = titles[name][1];
}

document.addEventListener('click', event => {
  const nav = event.target.closest('[data-page]');
  const go = event.target.closest('[data-go]');
  if (nav) showPage(nav.dataset.page);
  if (go) showPage(go.dataset.go);
});

function toast(message, error = false) {
  const node = $('#toast');
  node.textContent = message;
  node.className = error ? 'show error' : 'show';
  clearTimeout(node.timer);
  node.timer = setTimeout(() => { node.className = ''; }, 3500);
}

async function request(url, options) {
  const response = await fetch(url, options);
  const data = await response.json();
  if (!response.ok) throw new Error(data.error || '请求失败');
  return data;
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

function selectedCompany() { return (catalog.available_companies || []).find(item => item.id === $('#company').value); }
function selectedAccess() { const company = selectedCompany(); return company && company.access.find(item => item.id === $('#access').value); }
function selectedAgent() { return (catalog.agents || []).find(item => item.id === $('#agent').value); }

function fillModels(preferred) {
  const access = selectedAccess();
  setOptions($('#model'), access ? access.models : [], preferred, item => item.label);
  renderSelection();
}

function fillAccess(preferredAccess, preferredModel) {
  const company = selectedCompany();
  setOptions($('#access'), company ? company.access : [], preferredAccess, item => item.label);
  fillModels(preferredModel);
}

function renderAgentAvailability() {
  const available = catalog.available_companies || [];
  $('#agent-empty').classList.toggle('hidden', available.length > 0);
  $('#agent-workspace').classList.toggle('hidden', available.length === 0);
  if (!available.length) return;
  const selection = catalog.selection || {};
  setOptions($('#company'), available, selection.company, item => item.label);
  setOptions($('#agent'), catalog.agents || [], selection.agent, item => item.label);
  fillAccess(selection.access, selection.model);
}

function renderSelection() {
  if (!catalog || !(catalog.available_companies || []).length) return;
  const company = selectedCompany();
  const access = selectedAccess();
  const agent = selectedAgent();
  const model = access && access.models.find(item => item.id === $('#model').value);
  const valid = Boolean(company && access && agent && model);
  $('#create-session').disabled = !valid;
  $('#access-note').textContent = valid ? `${company.label} · ${access.label} · ${model.label} · ${agent.description}` : '请选择完整配置。';
}

async function loadInfo() {
  catalog = await request('/api/info');
  $('#health').textContent = '服务正常';
  $('#health-dot').className = 'ready';
  $('#version').textContent = `v${catalog.version}`;
  $('#workspace').textContent = catalog.workspace;
  renderAgentAvailability();
  renderSettings();
  renderDashboard();
}

async function loadSessions(openSelected = true) {
  const data = await request('/api/sessions?limit=100');
  sessions = data.sessions || [];
  renderSessionList();
  renderDashboard();
  if (!openSelected) return;
  const selected = localStorage.getItem('gohermit.session');
  if (selected && sessions.some(item => item.id === selected)) await openSession(selected);
  else showNewSession();
}

function renderDashboard() {
  if (!catalog) return;
  const connected = [];
  for (const company of catalog.companies || []) {
    for (const access of company.access) {
      const status = catalog.auth_status[access.id];
      if (status && status.configured) connected.push({company, access, status});
    }
  }
  $('#metric-service').textContent = '正常';
  $('#metric-providers').textContent = String(connected.length);
  $('#metric-sessions').textContent = String(sessions.length);
  $('#metric-run').textContent = catalog.active ? '运行中' : '空闲';
  const providers = $('#connected-list');
  providers.replaceChildren();
  if (!connected.length) providers.append(placeholder('尚未接入模型服务。'));
  for (const item of connected) providers.append(providerRow(item));
  const recent = $('#recent-sessions');
  recent.replaceChildren();
  if (!sessions.length) recent.append(placeholder('还没有会话。创建一个会话开始开发。'));
  for (const item of sessions.slice(0, 5)) {
    const button = document.createElement('button');
    button.className = 'session-card';
    button.innerHTML = '<strong></strong><span></span><small></small>';
    button.querySelector('strong').textContent = item.title;
    button.querySelector('span').textContent = statusText(item.last_run_status);
    button.querySelector('small').textContent = new Date(item.updated_at).toLocaleString();
    button.addEventListener('click', async () => { showPage('agent'); await openSession(item.id); });
    recent.append(button);
  }
}

function providerRow(item) {
  const row = document.createElement('div');
  row.className = 'provider-row';
  row.innerHTML = '<div class="identity"><span class="provider-logo"></span><div><strong></strong><small></small></div></div><span class="status-badge ready">已连接</span>';
  row.querySelector('.provider-logo').textContent = item.company.label.slice(0, 1);
  row.querySelector('strong').textContent = `${item.company.label} · ${item.access.label}`;
  row.querySelector('small').textContent = item.status.source || '服务端凭据';
  return row;
}

function renderSessionList() {
  const list = $('#session-list');
  list.replaceChildren();
  if (!sessions.length) list.append(placeholder('还没有会话。'));
  for (const item of sessions) {
    const button = document.createElement('button');
    button.className = `session-item ${current && current.session.id === item.id ? 'active' : ''}`;
    button.innerHTML = '<strong></strong><span></span><small></small>';
    button.querySelector('strong').textContent = item.title;
    button.querySelector('span').textContent = statusText(item.last_run_status);
    button.querySelector('small').textContent = new Date(item.updated_at).toLocaleString();
    button.addEventListener('click', () => openSession(item.id));
    list.append(button);
  }
}

function placeholder(text) {
  const node = document.createElement('p');
  node.className = 'placeholder';
  node.textContent = text;
  return node;
}

function showNewSession() {
  closeEvents();
  current = null;
  localStorage.removeItem('gohermit.session');
  $('#new-session-form').classList.remove('hidden');
  $('#conversation').classList.add('hidden');
  renderSessionList();
}

async function createSession() {
  const payload = {company: $('#company').value, access: $('#access').value, model: $('#model').value, agent: $('#agent').value};
  const session = await request('/api/sessions', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(payload)});
  await loadSessions(false);
  await openSession(session.id);
  return session.id;
}

async function openSession(id) {
  closeEvents();
  current = await request(`/api/sessions/${id}`);
  localStorage.setItem('gohermit.session', id);
  $('#new-session-form').classList.add('hidden');
  $('#conversation').classList.remove('hidden');
  $('#conversation-title').textContent = current.session.title;
  const sel = current.session.selection || {};
  $('#conversation-meta').textContent = [sel.company, sel.access, sel.model, sel.agent].filter(Boolean).join(' · ');
  renderMessages(current.messages || []);
  renderRunState();
  lastSequence = 0;
  $('#events').replaceChildren(placeholder('正在加载运行活动…'));
  $('#activity-count').textContent = '0';
  connectEvents(id);
  renderSessionList();
}

function renderMessages(messages) {
  const root = $('#messages');
  root.replaceChildren();
  if (!messages.length) root.append(placeholder('发送第一条消息开始开发。'));
  for (const message of messages) addMessage(message.role, message.content, false);
  root.scrollTop = root.scrollHeight;
}

function addMessage(role, content, scroll = true) {
  const root = $('#messages');
  const empty = root.querySelector('.placeholder');
  if (empty) root.replaceChildren();
  const row = document.createElement('div');
  row.className = `message ${role}`;
  const label = document.createElement('span');
  label.textContent = role === 'user' ? '你' : 'GoHermit';
  const body = document.createElement('div');
  body.textContent = content;
  row.append(label, body);
  root.append(row);
  if (scroll) root.scrollTop = root.scrollHeight;
  return body;
}

function activeRun() {
  if (!current || !current.session.active_run_id) return null;
  return (current.session.runs || []).find(run => run.id === current.session.active_run_id);
}

function renderRunState() {
  const run = activeRun();
  const status = run ? run.status : ((current.session.runs || []).at(-1) || {}).status;
  $('#run-status').textContent = statusText(status);
  $('#run-status').className = `status-badge ${status === 'completed' ? 'ready' : status === 'failed' || status === 'cancelled' ? 'error' : ''}`;
  const busy = run && ['queued', 'running', 'verifying'].includes(run.status);
  $('#send').disabled = Boolean(busy);
  $('#cancel-run').classList.toggle('hidden', !busy);
  $('#resume-run').classList.toggle('hidden', !(run && run.status === 'interrupted'));
}

function statusText(status) {
  return ({queued: '等待中', running: '运行中', verifying: '验证中', completed: '已完成', failed: '失败', cancelled: '已取消', interrupted: '可恢复', open: '空闲'})[status] || '空闲';
}

async function sendMessage() {
  const message = $('#task').value.trim();
  if (!message) { $('#task').focus(); return; }
  if (!current) await createSession();
  const id = current.session.id;
  connectEvents(id);
  $('#send').disabled = true;
  addMessage('user', message);
  $('#task').value = '';
  try {
    const result = await request(`/api/sessions/${id}/runs`, {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({message})});
    current.session.active_run_id = result.run_id;
    current.session.runs = current.session.runs || [];
    current.session.runs.push({id: result.run_id, status: 'queued', message});
    renderRunState();
    await loadInfo();
  } catch (error) {
    toast(error.message, true);
    await refreshCurrent();
  }
}

function connectEvents(sessionID) {
  if (eventSource && eventSource.sessionID === sessionID) return;
  closeEvents();
  eventSource = new EventSource(`/api/sessions/${sessionID}/events?after=${lastSequence}`);
  eventSource.sessionID = sessionID;
  const types = ['task_started', 'turn_started', 'model_started', 'model_delta', 'model_completed', 'tool_started', 'tool_completed', 'permission_required', 'checkpoint_saved', 'run_verifying', 'run_interrupted', 'workspace_changed', 'memory_updated', 'session_updated', 'task_completed', 'task_failed', 'task_cancelled'];
  for (const type of types) eventSource.addEventListener(type, event => consumeEvent(type, event));
  eventSource.onerror = () => { if (current && current.session.id === sessionID) $('#run-hint').textContent = '事件连接正在重试…'; };
}

function closeEvents() {
  if (eventSource) eventSource.close();
  eventSource = null;
  streamingBubble = null;
}

function consumeEvent(type, sourceEvent) {
  let event;
  try { event = JSON.parse(sourceEvent.data); } catch (_) { return; }
  if (event.sequence) lastSequence = Math.max(lastSequence, event.sequence);
  if (type === 'model_delta') {
    if (!streamingBubble) streamingBubble = addMessage('assistant', '');
    streamingBubble.textContent += event.message || '';
    $('#messages').scrollTop = $('#messages').scrollHeight;
    return;
  }
  if (type === 'model_started') streamingBubble = null;
  addActivity(type, event.error || event.message || event.tool || '');
  if (type === 'run_verifying') setTransientStatus('验证中');
  if (type === 'task_started') setTransientStatus('运行中');
  if (['task_completed', 'task_failed', 'task_cancelled'].includes(type)) {
    streamingBubble = null;
    setTimeout(refreshCurrent, 120);
  }
}

function addActivity(type, message) {
  const root = $('#events');
  const empty = root.querySelector('.placeholder');
  if (empty) root.replaceChildren();
  const row = document.createElement('div');
  row.className = `event ${type.includes('failed') || type.includes('cancelled') ? 'error' : ''}`;
  const label = document.createElement('span');
  label.className = 'type';
  label.textContent = type;
  const body = document.createElement('span');
  body.className = 'body';
  body.textContent = message || '—';
  row.append(label, body);
  root.append(row);
  $('#activity-count').textContent = String(root.querySelectorAll('.event').length);
  root.scrollTop = root.scrollHeight;
}

function setTransientStatus(text) {
  $('#run-status').textContent = text;
  $('#run-status').className = 'status-badge';
  $('#send').disabled = true;
  $('#cancel-run').classList.remove('hidden');
}

async function refreshCurrent() {
  if (!current) return;
  const id = current.session.id;
  current = await request(`/api/sessions/${id}`);
  $('#conversation-title').textContent = current.session.title;
  const sel = current.session.selection || {};
  $('#conversation-meta').textContent = [sel.company, sel.access, sel.model, sel.agent].filter(Boolean).join(' · ');
  renderMessages(current.messages || []);
  renderRunState();
  await loadSessions(false);
  await loadInfo();
}

async function cancelRun() {
  const run = activeRun();
  if (!run) return;
  try { await request(`/api/sessions/${current.session.id}/runs/${run.id}/cancel`, {method: 'POST'}); }
  catch (error) { toast(error.message, true); }
}

async function resumeRun() {
  const run = activeRun();
  if (!run) return;
  try {
    connectEvents(current.session.id);
    await request(`/api/sessions/${current.session.id}/runs/${run.id}/resume`, {method: 'POST'});
    setTransientStatus('恢复中');
  } catch (error) { toast(error.message, true); }
}

function renderSettings() {
  const root = $('#settings-list');
  root.replaceChildren();
  for (const company of catalog.companies || []) {
    const group = document.createElement('section');
    group.className = 'company-group';
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
  card.dataset.provider = access.id;
  const top = document.createElement('div');
  top.className = 'access-top';
  top.innerHTML = '<div class="access-title"><span class="provider-logo"></span><div><h4></h4><p></p></div></div><span class="status-badge"></span>';
  top.querySelector('.provider-logo').textContent = company.label.slice(0, 1);
  top.querySelector('h4').textContent = access.label;
  top.querySelector('p').textContent = status.configured ? `${status.detail} 来源：${status.source}` : status.detail;
  const badge = top.querySelector('.status-badge');
  badge.classList.add(status.configured ? 'ready' : 'error');
  badge.textContent = status.configured ? '已连接' : '未连接';
  card.append(top);
  const actions = document.createElement('div');
  actions.className = 'access-actions';
  if (access.auth_type === 'api_key') {
    actions.innerHTML = '<div class="key-controls"><input type="password" autocomplete="off" placeholder="粘贴 API Key"><button class="primary">保存 Key</button></div>';
    actions.querySelector('button').addEventListener('click', () => saveKey(access.id, actions.querySelector('input')));
    if (status.configured && status.source === 'GoHermit 设置') actions.append(deleteButton(access.id, '移除'));
  } else if (status.configured) {
    const box = document.createElement('div');
    box.className = 'login-box';
    const text = document.createElement('span');
    text.textContent = 'Codex 登录有效，可以直接用于新会话。';
    box.append(text);
    if ((status.source || '').includes('GoHermit')) box.append(deleteButton(access.id, '退出登录'));
    actions.append(box);
  } else {
    const box = document.createElement('div');
    box.className = 'login-box';
    const text = document.createElement('span');
    text.textContent = '使用浏览器完成 OpenAI Codex 设备登录。';
    const button = document.createElement('button');
    button.className = 'primary';
    button.textContent = '登录 Codex';
    button.addEventListener('click', () => startCodexLogin(button, card));
    box.append(text, button);
    actions.append(box);
  }
  card.append(actions);
  return card;
}

function deleteButton(provider, label) {
  const button = document.createElement('button');
  button.className = 'danger';
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
    text.textContent = '打开登录页面并输入代码：';
    const code = document.createElement('strong');
    code.textContent = login.user_code;
    const link = document.createElement('a');
    link.href = login.verification_url;
    link.target = '_blank';
    link.rel = 'noopener noreferrer';
    link.textContent = '打开 OpenAI 登录页面 →';
    box.append(text, code, link);
    card.querySelector('.access-actions').append(box);
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

$('#company').addEventListener('change', () => fillAccess('', ''));
$('#access').addEventListener('change', () => fillModels(''));
$('#model').addEventListener('change', renderSelection);
$('#agent').addEventListener('change', renderSelection);
$('#new-session').addEventListener('click', showNewSession);
$('#create-session').addEventListener('click', async () => { try { await createSession(); } catch (error) { toast(error.message, true); } });
$('#send').addEventListener('click', sendMessage);
$('#cancel-run').addEventListener('click', cancelRun);
$('#resume-run').addEventListener('click', resumeRun);
$('#task').addEventListener('keydown', event => { if ((event.ctrlKey || event.metaKey) && event.key === 'Enter') sendMessage(); });

(async function boot() {
  try { await loadInfo(); await loadSessions(); }
  catch (error) { $('#health').textContent = '连接异常'; toast(error.message, true); }
})();
