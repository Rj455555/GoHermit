const $ = selector => document.querySelector(selector);
const run = $('#run');
const events = $('#events');
const health = $('#health');
const companySelect = $('#company');
const accessSelect = $('#access');
const modelSelect = $('#model');
const agentSelect = $('#agent');
let catalog = null;
let active = false;

function setOptions(select, items, selected, label) {
  select.replaceChildren();
  for (const item of items) {
    const option = document.createElement('option');
    option.value = item.id;
    option.textContent = label(item);
    select.append(option);
  }
  if (items.some(item => item.id === selected)) select.value = selected;
}

function selectedCompany() {
  return catalog.companies.find(item => item.id === companySelect.value);
}

function selectedAccess() {
  const company = selectedCompany();
  return company && company.access.find(item => item.id === accessSelect.value);
}

function selectedAgent() {
  return catalog.agents.find(item => item.id === agentSelect.value);
}

function fillModels(preferred) {
  const access = selectedAccess();
  setOptions(modelSelect, access ? access.models : [], preferred, item => item.label);
  renderSelection();
}

function fillAccess(preferredAccess, preferredModel) {
  const company = selectedCompany();
  setOptions(accessSelect, company ? company.access : [], preferredAccess, item => item.label);
  fillModels(preferredModel);
}

function setupCatalog(data) {
  catalog = data;
  const current = data.selection || {};
  setOptions(companySelect, data.companies, current.company, item => item.label);
  setOptions(agentSelect, data.agents, current.agent, item => item.label);
  fillAccess(current.access, current.model);
}

function renderSelection() {
  if (!catalog) return;
  const company = selectedCompany();
  const access = selectedAccess();
  const agent = selectedAgent();
  const model = access && access.models.find(item => item.id === modelSelect.value);
  if (!company || !access || !agent || !model) {
    run.disabled = true;
    return;
  }
  $('#channel').textContent = company.label + ' · ' + access.label;
  $('#protocol').textContent = ['codex', 'openai', 'openai-api', 'openai-codex'].includes(model.provider) ? 'Responses API' : 'Chat Completions';
  const status = catalog.auth_status[access.id] || {configured: false, detail: '未检测到认证'};
  const configured = Boolean(status.configured);
  const key = $('#key');
  const note = $('#access-note');
  if (!access.supported) {
    key.textContent = '尚未启用';
    key.className = 'warn';
    note.textContent = 'Codex Plan 与 API 是不同认证通道；当前容器尚未配置受支持的 Codex 客户端桥接。';
  } else if (!configured) {
    key.textContent = access.auth_type === 'api_key' ? '缺失：' + access.api_key_env : '未登录';
    key.className = 'warn';
    note.textContent = access.description + ' ' + status.detail;
  } else {
    key.textContent = '已配置';
    key.className = 'ready';
    note.textContent = access.description + ' ' + agent.description;
  }
  run.disabled = active || !access.supported || !configured;
}

async function info(refreshOnly = false) {
  try {
    const response = await fetch('/api/info');
    const data = await response.json();
    if (!response.ok) throw new Error(data.error);
    active = data.active;
    if (!refreshOnly || !catalog) setupCatalog(data);
    else {
      catalog.credentials = data.credentials;
      catalog.auth_status = data.auth_status;
      catalog.companies = data.companies;
      catalog.agents = data.agents;
      renderSelection();
    }
    health.textContent = active ? '运行中' : '服务正常';
    health.className = 'pill';
  } catch (error) {
    health.textContent = '配置异常';
    health.className = 'pill muted';
    add('error', error.message, 'error');
  }
}

function add(type, message, klass = '') {
  if (events.querySelector('.empty')) events.replaceChildren();
  const row = document.createElement('div');
  row.className = 'event ' + klass;
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
  let type = 'message';
  let data = '';
  for (const line of block.split('\n')) {
    if (line.startsWith('event:')) type = line.slice(6).trim();
    if (line.startsWith('data:')) data += line.slice(5).trim();
  }
  if (!data) return;
  try {
    const event = JSON.parse(data);
    const message = event.error || event.message || (event.tool ? event.tool + (event.data ? ' ' + JSON.stringify(event.data) : '') : '');
    add(type, message, type.includes('failed') || type.includes('cancelled') ? 'error' : type.includes('tool') ? 'tool' : '');
  } catch (error) {
    add('protocol_error', error.message, 'error');
  }
}

companySelect.addEventListener('change', () => fillAccess('', ''));
accessSelect.addEventListener('change', () => fillModels(''));
modelSelect.addEventListener('change', renderSelection);
agentSelect.addEventListener('change', renderSelection);

run.addEventListener('click', async () => {
  const task = $('#task').value.trim();
  if (!task) return $('#task').focus();
  active = true;
  renderSelection();
  health.textContent = '运行中';
  events.replaceChildren();
  try {
    const response = await fetch('/api/run', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({task, company: companySelect.value, access: accessSelect.value, model: modelSelect.value, agent: agentSelect.value})
    });
    if (!response.ok) {
      const data = await response.json();
      throw new Error(data.error || '请求失败');
    }
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';
    while (true) {
      const {value, done} = await reader.read();
      buffer += decoder.decode(value || new Uint8Array(), {stream: !done});
      const parts = buffer.split('\n\n');
      buffer = parts.pop();
      parts.forEach(consume);
      if (done) {
        if (buffer) consume(buffer);
        break;
      }
    }
  } catch (error) {
    add('error', error.message, 'error');
  } finally {
    active = false;
    await info(true);
  }
});

$('#clear').addEventListener('click', () => {
  events.innerHTML = '<p class="empty">尚未运行任务。</p>';
});

info();
