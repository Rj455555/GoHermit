const $ = selector => document.querySelector(selector);
let catalog = null;
let active = false;
let loginTimer = null;

const titles = {
  dashboard: ['概览', '查看服务和模型接入状态'],
  run: ['运行 Agent', '选择可用模型并交付一个开发任务'],
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
  window.clearTimeout(node.timer);
  node.timer = window.setTimeout(() => { node.className = ''; }, 3000);
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
function selectedAgent() { return catalog.agents.find(item => item.id === $('#agent').value); }

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

function renderRun() {
  const available = catalog.available_companies || [];
  $('#run-empty').classList.toggle('hidden', available.length > 0);
  $('#run-content').classList.toggle('hidden', available.length === 0);
  if (!available.length) return;
  const current = catalog.selection || {};
  setOptions($('#company'), available, current.company, item => item.label);
  setOptions($('#agent'), catalog.agents || [], current.agent, item => item.label);
  fillAccess(current.access, current.model);
}

function renderSelection() {
  if (!catalog || !(catalog.available_companies || []).length) return;
  const company = selectedCompany();
  const access = selectedAccess();
  const agent = selectedAgent();
  const model = access && access.models.find(item => item.id === $('#model').value);
  const valid = Boolean(company && access && agent && model);
  $('#run').disabled = active || !valid;
  if (!valid) return;
  const status = catalog.auth_status[access.id];
  $('#access-note').textContent = `${company.label} · ${access.label} · ${model.label}。${agent.description}`;
  $('#access-badge').textContent = status && status.source ? status.source : '可用';
}

function renderDashboard() {
  const connected = [];
  for (const company of catalog.companies || []) {
    for (const access of company.access) {
      const status = catalog.auth_status[access.id];
      if (status && status.configured) connected.push({company, access, status});
    }
  }
  $('#metric-service').textContent = '正常';
  $('#metric-providers').textContent = String(connected.length);
  $('#metric-run').textContent = catalog.active ? '运行中' : '空闲';
  $('#workspace').textContent = catalog.workspace;
  const list = $('#connected-list');
  list.replaceChildren();
  if (!connected.length) {
    const empty = document.createElement('p');
    empty.className = 'placeholder';
    empty.textContent = '尚未接入模型服务。';
    list.append(empty);
    return;
  }
  for (const item of connected) {
    const row = document.createElement('div');
    row.className = 'provider-row';
    const identity = document.createElement('div');
    identity.className = 'identity';
    const logo = document.createElement('span');
    logo.className = 'provider-logo';
    logo.textContent = item.company.label.slice(0, 1);
    const copy = document.createElement('div');
    const title = document.createElement('strong');
    title.textContent = `${item.company.label} · ${item.access.label}`;
    const source = document.createElement('small');
    source.textContent = item.status.source;
    copy.append(title, source);
    identity.append(logo, copy);
    const badge = document.createElement('span');
    badge.className = 'status-badge ready';
    badge.textContent = '已连接';
    row.append(identity, badge);
    list.append(row);
  }
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
  top.innerHTML = `<div class="access-title"><span class="provider-logo">${company.label.slice(0, 1)}</span><div><h4></h4><p></p></div></div><span class="status-badge ${status.configured ? 'ready' : 'error'}">${status.configured ? '已连接' : '未连接'}</span>`;
  top.querySelector('h4').textContent = access.label;
  top.querySelector('p').textContent = status.configured ? `${status.detail} 来源：${status.source}` : status.detail;
  card.append(top);
  const actions = document.createElement('div');
  actions.className = 'access-actions';
  if (access.auth_type === 'api_key') {
    actions.innerHTML = `<div class="key-controls"><input type="password" autocomplete="off" placeholder="粘贴 API Key" aria-label="${access.label} API Key" data-key-input="${access.id}"><button class="primary" data-save-key="${access.id}" data-testid="save-${access.id}">保存 Key</button>${status.configured && status.source === 'GoHermit 设置' ? `<button class="danger" data-delete="${access.id}">移除</button>` : ''}</div>`;
  } else if (status.configured) {
    actions.innerHTML = `<div class="login-box"><span>Codex 订阅登录有效，可以直接使用。</span>${status.source === 'GoHermit settings' || status.source === 'GoHermit 设置' ? `<button class="danger" data-delete="${access.id}">退出登录</button>` : ''}</div>`;
  } else {
    actions.innerHTML = `<div class="login-box"><span>使用浏览器完成 OpenAI Codex 设备登录。</span><button class="primary" data-codex-login data-testid="codex-login">登录 Codex</button></div><div class="device-host"></div>`;
  }
  card.append(actions);
  return card;
}

$('#settings-list').addEventListener('click', async event => {
  const save = event.target.closest('[data-save-key]');
  const remove = event.target.closest('[data-delete]');
  const login = event.target.closest('[data-codex-login]');
  if (save) await saveKey(save.dataset.saveKey);
  if (remove) await deleteCredential(remove.dataset.delete);
  if (login) await startCodexLogin(login);
});

async function saveKey(provider) {
  const input = document.querySelector(`[data-key-input="${provider}"]`);
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

async function startCodexLogin(button) {
  button.disabled = true;
  button.textContent = '正在准备…';
  try {
    const session = await request('/api/settings/providers/openai-codex/login', {method: 'POST'});
    showDeviceCode(session);
    pollLogin(session.id);
  } catch (error) {
    button.disabled = false;
    button.textContent = '登录 Codex';
    toast(error.message, true);
  }
}

function showDeviceCode(session) {
  const host = document.querySelector('[data-provider="openai-codex"] .device-host');
  if (!host) return;
  host.innerHTML = '';
  const box = document.createElement('div');
  box.className = 'device-code';
  const text = document.createElement('span');
  text.textContent = '打开登录页面并输入代码：';
  const code = document.createElement('strong');
  code.textContent = session.user_code;
  const link = document.createElement('a');
  link.href = session.verification_url;
  link.target = '_blank';
  link.rel = 'noopener noreferrer';
  link.textContent = '打开 OpenAI 登录页面 ↗';
  box.append(text, code, link);
  host.append(box);
}

function pollLogin(id) {
  window.clearTimeout(loginTimer);
  loginTimer = window.setTimeout(async () => {
    try {
      const session = await request(`/api/settings/logins/${id}`);
      if (session.status === 'approved') {
        toast('Codex 登录成功');
        await loadInfo();
        return;
      }
      if (session.status === 'error' || session.status === 'expired') {
        toast(session.error || 'Codex 登录失败', true);
        await loadInfo();
        return;
      }
      pollLogin(id);
    } catch (error) { toast(error.message, true); }
  }, 2000);
}

async function request(url, options) {
  const response = await fetch(url, options);
  const data = await response.json();
  if (!response.ok) throw new Error(data.error || '请求失败');
  return data;
}

async function loadInfo() {
  try {
    catalog = await request('/api/info');
    active = catalog.active;
    $('#health').textContent = '服务正常';
    $('#health-dot').className = 'ready';
    $('#version').textContent = `v${catalog.version}`;
    renderDashboard();
    renderRun();
    renderSettings();
  } catch (error) {
    $('#health').textContent = '连接异常';
    $('#health-dot').className = '';
    toast(error.message, true);
  }
}

function addEvent(type, message, className = '') {
  const events = $('#events');
  const empty = events.querySelector('.placeholder');
  if (empty) events.replaceChildren();
  const row = document.createElement('div');
  row.className = `event ${className}`;
  const eventType = document.createElement('span');
  eventType.className = 'type';
  eventType.textContent = type;
  const body = document.createElement('span');
  body.className = 'body';
  body.textContent = message || '—';
  row.append(eventType, body);
  events.append(row);
  events.scrollTop = events.scrollHeight;
}

function consume(block) {
  let type = 'message'; let data = '';
  for (const line of block.split('\n')) {
    if (line.startsWith('event:')) type = line.slice(6).trim();
    if (line.startsWith('data:')) data += line.slice(5).trim();
  }
  if (!data) return;
  try {
    const event = JSON.parse(data);
    const message = event.error || event.message || (event.tool ? event.tool + (event.data ? ` ${JSON.stringify(event.data)}` : '') : '');
    addEvent(type, message, type.includes('failed') || type.includes('cancelled') ? 'error' : '');
  } catch (error) { addEvent('protocol_error', error.message, 'error'); }
}

$('#company').addEventListener('change', () => fillAccess('', ''));
$('#access').addEventListener('change', () => fillModels(''));
$('#model').addEventListener('change', renderSelection);
$('#agent').addEventListener('change', renderSelection);

$('#run').addEventListener('click', async () => {
  const task = $('#task').value.trim();
  if (!task) { $('#task').focus(); return; }
  active = true; renderSelection(); $('#events').replaceChildren();
  try {
    const response = await fetch('/api/run', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({task, company: $('#company').value, access: $('#access').value, model: $('#model').value, agent: $('#agent').value})});
    if (!response.ok) { const data = await response.json(); throw new Error(data.error || '请求失败'); }
    const reader = response.body.getReader(); const decoder = new TextDecoder(); let buffer = '';
    while (true) {
      const {value, done} = await reader.read();
      buffer += decoder.decode(value || new Uint8Array(), {stream: !done});
      const parts = buffer.split('\n\n'); buffer = parts.pop(); parts.forEach(consume);
      if (done) { if (buffer) consume(buffer); break; }
    }
  } catch (error) { addEvent('error', error.message, 'error'); }
  finally { active = false; await loadInfo(); }
});

$('#clear').addEventListener('click', () => { $('#events').innerHTML = '<p class="placeholder">尚未运行任务。</p>'; });
loadInfo();
