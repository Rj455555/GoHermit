let loopDefinitions = [];
let loopInvocations = [];
let loopInvocationCache = new Map();
let selectedLoop = null;
let selectedLoopInvocation = null;
let loopDryRunReport = null;
let loopSessionState = null;
let loopEventStream = null;
let loopRuntimeEvents = [];
let loopTeamTemplate = null;
let loopEmployees = [];

const terminalInvocationStatuses = new Set(['completed', 'skipped', 'blocked', 'failed', 'cancelled']);
const loopEventTypes = [
  'task_started', 'turn_started', 'model_started', 'model_completed',
  'tool_started', 'tool_completed', 'permission_required', 'checkpoint_saved',
  'run_verifying', 'run_interrupted', 'workspace_changed', 'session_updated',
  'plan_created', 'plan_updated', 'mission_started', 'mission_completed',
  'mission_failed', 'work_item_started', 'work_item_completed',
  'work_item_failed', 'approval_requested', 'approval_decided',
  'approval_expired', 'approval_consumed', 'task_completed', 'task_failed',
  'task_cancelled'
];

function switchWorkbenchView(view) {
  const target = ['agent', 'dashboard', 'employees', 'employee-tasks', 'loops'].includes(view) ? view : 'agent';
  if (target !== 'employee-tasks' && typeof closeTaskEvents === 'function') closeTaskEvents();
  $('#agent-view').classList.toggle('hidden', target !== 'agent');
  $('#dashboard-view').classList.toggle('hidden', target !== 'dashboard');
  $('#employees-view').classList.toggle('hidden', target !== 'employees');
  $('#employee-tasks-view').classList.toggle('hidden', target !== 'employee-tasks');
  $('#loops-view').classList.toggle('hidden', target !== 'loops');
  $('#app').classList.toggle('view-dashboard', target === 'dashboard');
  $('#app').classList.toggle('view-employees', target === 'employees');
  $('#app').classList.toggle('view-employee-tasks', target === 'employee-tasks');
  $('#app').classList.toggle('view-loops', target === 'loops');
  $('#dashboard-button').classList.toggle('active', target === 'dashboard');
  $('#employees-button').classList.toggle('active', target === 'employees');
  $('#employee-tasks-button').classList.toggle('active', target === 'employee-tasks');
  $('#tasks-button').classList.toggle('active', target === 'agent');
  $('#loops-button').classList.toggle('active', target === 'loops');
  localStorage.setItem('gohermit.view', target);
  closeMobileSidebar();
  if (target === 'dashboard') renderLoopDashboard();
  if (target === 'employees' && typeof loadEmployees === 'function') loadEmployees().catch(error => showEmployeeError(error));
  if (target === 'employee-tasks' && typeof loadEmployeeTaskWorkbench === 'function') loadEmployeeTaskWorkbench().catch(error => showTaskError(error));
  if (target === 'loops') loadLoops().catch(error => toast(error.message, true));
}

async function loadLoops(openSaved = true) {
  const data = await request('/api/loops');
  loopDefinitions = data.loops || [];
  renderLoopList();
  await loadInvocationSummaries();
  if (!openSaved || selectedLoop) {
    renderLoopDashboard();
    return;
  }
  const savedID = localStorage.getItem('gohermit.loop');
  const saved = loopDefinitions.find(definition => definition.id === savedID);
  if (saved) await openLoop(saved.id, true);
  renderLoopDashboard();
}

async function loadInvocationSummaries() {
  const results = await Promise.all(loopDefinitions.map(async definition => {
    try {
      const data = await request(`/api/loops/${encodeURIComponent(definition.id)}/invocations?limit=20`);
      return [definition.id, data.invocations || []];
    } catch (_) {
      return [definition.id, []];
    }
  }));
  loopInvocationCache = new Map(results);
}

function renderLoopList() {
  const root = $('#loop-list');
  root.replaceChildren();
  const query = $('#loop-search').value.trim().toLowerCase();
  const visible = loopDefinitions.filter(definition => {
    const haystack = `${definition.id} ${definition.name} ${definition.description || ''}`.toLowerCase();
    return !query || haystack.includes(query);
  });
  if (!visible.length) {
    const empty = document.createElement('div');
    empty.className = 'sidebar-empty';
    empty.textContent = loopDefinitions.length ? '没有匹配的 Loop' : '还没有 Loop Definition';
    root.append(empty);
    return;
  }
  for (const definition of visible) {
    const button = document.createElement('button');
    button.className = `loop-list-item ${selectedLoop && selectedLoop.id === definition.id ? 'active' : ''}`;
    button.dataset.loopId = definition.id;
    button.innerHTML = '<i></i><div><strong></strong><span></span></div>';
    button.querySelector('i').className = definition.enabled ? 'enabled' : '';
    button.querySelector('strong').textContent = definition.name;
    button.querySelector('span').textContent = `${definition.id} · r${definition.revision}`;
    button.addEventListener('click', () => openLoop(definition.id));
    root.append(button);
  }
}

function configuredSelection() {
  const companies = (catalog && catalog.available_companies) || [];
  const preferred = (catalog && catalog.selection) || {};
  const company = companies.find(item => item.id === preferred.company) || companies[0];
  const access = company && (company.access || []).find(item => item.id === preferred.access) || (company && company.access[0]);
  const model = access && (access.models || []).find(item => item.id === preferred.model) || (access && access.models[0]);
  const agents = (catalog && catalog.agents) || [];
  const agent = agents.find(item => item.id === 'team') || agents.find(item => item.id === preferred.agent) || agents[0];
  return {
    company: company ? company.id : '',
    access: access ? access.id : '',
    model: model ? model.id : '',
    agent: agent ? agent.id : 'coding'
  };
}

function documentMaintenanceTemplate() {
  const selection = configuredSelection();
  const baseID = 'document-maintenance';
  let id = baseID;
  let suffix = 2;
  while (loopDefinitions.some(definition => definition.id === id)) id = `${baseID}-${suffix++}`;
  return {
    id,
    schema_version: 1,
    name: '文档维护 Loop',
    description: '检查 canonical AI 文档与代码、版本和路线图是否发生漂移。',
    workspace_identity: (catalog && catalog.workspace) || '',
    enabled: true,
    task_source: {
      type: 'fixed_prompt',
      prompt: '检查 AGENTS.md、docs/ai/context.md、docs/ai/next-development-plan.md、docs/roadmap.md 和 CHANGELOG.md 与当前代码及版本是否一致。只报告漂移与建议的文档修正，不提交、不推送、不创建 PR。'
    },
    agent_selection: selection,
    team_template_ref: selection.agent === 'team' ? 'default' : '',
    plan_mode: 'review',
    verification_recipe: {
      checks: [{id: 'diff-check', command: ['git', 'diff', '--check'], required: true, timeout_seconds: 60}],
      independent_verifier: selection.agent === 'team',
      max_repair_attempts: 0
    },
    budget: {max_model_calls: 12, max_tokens: 120000, timeout_seconds: 1200},
    approval_policy: {require_for_mutation: false},
    workspace_policy: {read_only: true, require_clean_git: false},
    output_policy: {include_diff: false, max_report_bytes: 65536},
    revision: 0
  };
}

function newLoop() {
  selectedLoop = null;
  selectedLoopInvocation = null;
  loopDryRunReport = null;
  loopInvocations = [];
  closeLoopEvents();
  fillLoopForm(documentMaintenanceTemplate(), true);
  renderLoopHistory();
  renderLoopList();
  showLoopDefinition();
}

async function openLoop(id, restoreInvocation = false) {
  const definition = await request(`/api/loops/${encodeURIComponent(id)}`);
  selectedLoop = definition;
  loopDryRunReport = null;
  localStorage.setItem('gohermit.loop', id);
  fillLoopForm(definition, false);
  const history = await request(`/api/loops/${encodeURIComponent(id)}/invocations?limit=50`);
  loopInvocations = history.invocations || [];
  loopInvocationCache.set(id, loopInvocations);
  renderLoopHistory();
  renderLoopList();
  renderLoopDashboard();
  const savedInvocation = restoreInvocation && localStorage.getItem('gohermit.loop.invocation');
  if (savedInvocation && loopInvocations.some(invocation => invocation.id === savedInvocation)) {
    await openLoopInvocation(savedInvocation);
  } else {
    showLoopDefinition();
  }
}

function fillLoopSelects(definition) {
  const selection = definition.agent_selection || configuredSelection();
  const companies = (catalog && catalog.available_companies) || [];
  setOptions($('#loop-company'), companies, selection.company, item => item.label);
  const company = companies.find(item => item.id === $('#loop-company').value);
  setOptions($('#loop-access'), company ? company.access : [], selection.access, item => item.label);
  const access = company && company.access.find(item => item.id === $('#loop-access').value);
  setOptions($('#loop-model'), access ? access.models : [], selection.model, item => item.label);
  setOptions($('#loop-agent'), (catalog && catalog.agents) || [], selection.agent, item => item.label);
}

function fillLoopForm(definition, creating) {
  $('#loop-empty').classList.add('hidden');
  $('#loop-form').classList.remove('hidden');
  $('#loop-timeline-panel').classList.add('hidden');
  $('#loop-id').value = definition.id || '';
  $('#loop-id').disabled = !creating;
  $('#loop-name').value = definition.name || '';
  $('#loop-description').value = definition.description || '';
  $('#loop-workspace').value = definition.workspace_identity || ((catalog && catalog.workspace) || '');
  $('#loop-enabled').checked = definition.enabled !== false;
  $('#loop-prompt').value = (definition.task_source && definition.task_source.prompt) || '';
  fillLoopSelects(definition);
  $('#loop-team-template').value = definition.team_template_ref || '';
  $('#loop-plan-mode').value = definition.plan_mode || 'auto';
  $('#loop-read-only').checked = Boolean(definition.workspace_policy && definition.workspace_policy.read_only);
  $('#loop-clean-git').checked = Boolean(definition.workspace_policy && definition.workspace_policy.require_clean_git);
  $('#loop-approval').checked = Boolean(definition.approval_policy && definition.approval_policy.require_for_mutation);
  $('#loop-independent-verifier').checked = Boolean(definition.verification_recipe && definition.verification_recipe.independent_verifier);
  $('#loop-repairs').value = (definition.verification_recipe && definition.verification_recipe.max_repair_attempts) ?? 0;
  $('#loop-max-calls').value = (definition.budget && definition.budget.max_model_calls) || 10;
  $('#loop-max-tokens').value = (definition.budget && definition.budget.max_tokens) || 100000;
  $('#loop-timeout').value = (definition.budget && definition.budget.timeout_seconds) || 900;
  $('#loop-report-bytes').value = (definition.output_policy && definition.output_policy.max_report_bytes) || 65536;
  $('#loop-include-diff').checked = Boolean(definition.output_policy && definition.output_policy.include_diff);
  const checks = $('#loop-checks');
  checks.replaceChildren();
  for (const check of (definition.verification_recipe && definition.verification_recipe.checks) || []) addVerificationCheck(check);
  $('#loop-editor-title').textContent = creating ? 'New Loop Definition' : definition.name;
  $('#loop-editor-meta').textContent = creating ? '尚未保存' : `${definition.id} · revision ${definition.revision}`;
  $('#loop-save-hint').textContent = creating ? '保存后创建 revision 1' : `保存将创建 revision ${definition.revision + 1}`;
  $('#loop-dry-run').disabled = creating;
  $('#loop-start').disabled = true;
  $('#loop-dry-run-result').className = 'review-empty';
  $('#loop-dry-run-result').textContent = creating ? '先保存 Definition，再运行 Dry Run。' : 'Definition 已加载；运行 Dry Run 检查 readiness。';
  $('#loop-form-errors').classList.add('hidden');
  $('#loop-form-errors').textContent = '';
  renderTeamRolePreview();
  updateLoopJSONPreview();
}

function addVerificationCheck(check = {id: '', command: [''], required: true, timeout_seconds: 120}) {
  const row = document.createElement('article');
  row.className = 'verification-check';
  row.innerHTML = '<div class="check-head"><label>Check ID<input class="check-id" maxlength="128"></label><label>Timeout (s)<input class="check-timeout" type="number" min="1" max="3600"></label><label class="check-required"><input type="checkbox"><span>Required</span></label><button type="button" class="small-button danger-text check-remove">删除</button></div><div class="argv-list"></div><button type="button" class="small-button check-add-arg">添加 argv</button>';
  row.querySelector('.check-id').value = check.id || '';
  row.querySelector('.check-timeout').value = check.timeout_seconds || 120;
  row.querySelector('.check-required input').checked = check.required !== false;
  const args = check.command && check.command.length ? check.command : [''];
  for (const arg of args) addArgInput(row.querySelector('.argv-list'), arg);
  row.querySelector('.check-add-arg').addEventListener('click', () => {
    addArgInput(row.querySelector('.argv-list'), '');
    updateLoopJSONPreview();
  });
  row.querySelector('.check-remove').addEventListener('click', () => {
    row.remove();
    updateLoopJSONPreview();
  });
  $('#loop-checks').append(row);
}

function addArgInput(root, value) {
  const label = document.createElement('label');
  label.innerHTML = '<span>argv</span><input maxlength="8192"><button type="button" aria-label="删除参数">×</button>';
  label.querySelector('input').value = value;
  label.querySelector('button').addEventListener('click', () => {
    label.remove();
    updateLoopJSONPreview();
  });
  root.append(label);
}

function collectLoopDefinition() {
  const checks = [...document.querySelectorAll('#loop-checks .verification-check')].map(row => ({
    id: row.querySelector('.check-id').value.trim(),
    command: [...row.querySelectorAll('.argv-list input')].map(input => input.value).filter(value => value.length > 0),
    required: row.querySelector('.check-required input').checked,
    timeout_seconds: Number(row.querySelector('.check-timeout').value)
  }));
  return {
    id: $('#loop-id').value.trim(),
    schema_version: 1,
    name: $('#loop-name').value.trim(),
    description: $('#loop-description').value.trim(),
    workspace_identity: $('#loop-workspace').value.trim(),
    enabled: $('#loop-enabled').checked,
    task_source: {type: 'fixed_prompt', prompt: $('#loop-prompt').value.trim()},
    agent_selection: {
      company: $('#loop-company').value,
      access: $('#loop-access').value,
      model: $('#loop-model').value,
      agent: $('#loop-agent').value
    },
    team_template_ref: $('#loop-team-template').value.trim(),
    plan_mode: $('#loop-plan-mode').value,
    verification_recipe: {
      checks,
      independent_verifier: $('#loop-independent-verifier').checked,
      max_repair_attempts: Number($('#loop-repairs').value)
    },
    budget: {
      max_model_calls: Number($('#loop-max-calls').value),
      max_tokens: Number($('#loop-max-tokens').value),
      timeout_seconds: Number($('#loop-timeout').value)
    },
    approval_policy: {require_for_mutation: $('#loop-approval').checked},
    workspace_policy: {read_only: $('#loop-read-only').checked, require_clean_git: $('#loop-clean-git').checked},
    output_policy: {include_diff: $('#loop-include-diff').checked, max_report_bytes: Number($('#loop-report-bytes').value)},
    revision: selectedLoop ? selectedLoop.revision : 0
  };
}

function validateLoopForm(definition) {
  const errors = [];
  if (!/^[a-z0-9][a-z0-9._-]{0,127}$/.test(definition.id)) errors.push('Loop ID 只能使用小写字母、数字、点、下划线和连字符。');
  if (!definition.name) errors.push('名称不能为空。');
  if (!definition.workspace_identity) errors.push('Workspace identity 不能为空。');
  if (!definition.task_source.prompt) errors.push('固定 Prompt 不能为空。');
  const selection = definition.agent_selection;
  if (!selection.company || !selection.access || !selection.model || !selection.agent) errors.push('请选择可用的公司、接入方式、模型和 Agent。');
  if (definition.verification_recipe.checks.length > 16) errors.push('Verification checks 最多 16 条。');
  for (const [index, check] of definition.verification_recipe.checks.entries()) {
    if (!check.id) errors.push(`第 ${index + 1} 条检查缺少 ID。`);
    if (!check.command.length) errors.push(`第 ${index + 1} 条检查至少需要一个 argv。`);
    if (check.command.length > 8) errors.push(`第 ${index + 1} 条检查最多 8 个 argv。`);
    if (check.timeout_seconds < 1 || check.timeout_seconds > 3600) errors.push(`第 ${index + 1} 条检查超时必须为 1–3600 秒。`);
  }
  if (!definition.workspace_policy.read_only && !definition.verification_recipe.checks.some(check => check.required)) {
    errors.push('Mutation Loop 至少需要一个 required verification check。');
  }
  if (definition.budget.max_model_calls < 1 || definition.budget.max_model_calls > 1000) errors.push('Max model calls 必须为 1–1000。');
  if (definition.budget.max_tokens < 1 || definition.budget.max_tokens > 10000000) errors.push('Max tokens 必须为 1–10000000。');
  if (definition.budget.timeout_seconds < 1 || definition.budget.timeout_seconds > 86400) errors.push('总超时必须为 1–86400 秒。');
  if (definition.verification_recipe.max_repair_attempts < 0 || definition.verification_recipe.max_repair_attempts > 5) errors.push('Max repair attempts 必须为 0–5。');
  if (definition.output_policy.max_report_bytes < 1 || definition.output_policy.max_report_bytes > 1048576) errors.push('Max report bytes 必须为 1–1048576。');
  return errors;
}

function updateLoopJSONPreview() {
  if ($('#loop-form').classList.contains('hidden')) return;
  $('#loop-json-preview').textContent = JSON.stringify(collectLoopDefinition(), null, 2);
}

async function saveLoop(event) {
  event.preventDefault();
  const definition = collectLoopDefinition();
  const errors = validateLoopForm(definition);
  const errorBox = $('#loop-form-errors');
  if (errors.length) {
    errorBox.textContent = errors.join('\n');
    errorBox.classList.remove('hidden');
    return;
  }
  errorBox.classList.add('hidden');
  const editing = Boolean(selectedLoop);
  try {
    $('#loop-save').disabled = true;
    const saved = await request(editing ? `/api/loops/${encodeURIComponent(selectedLoop.id)}` : '/api/loops', {
      method: editing ? 'PUT' : 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify(definition)
    });
    selectedLoop = saved;
    loopDryRunReport = null;
    localStorage.setItem('gohermit.loop', saved.id);
    await loadLoops(false);
    await openLoop(saved.id);
    toast(editing ? `已保存 revision ${saved.revision}` : 'Loop Definition 已创建');
  } catch (error) {
    errorBox.textContent = error.message;
    errorBox.classList.remove('hidden');
  } finally {
    $('#loop-save').disabled = false;
  }
}

async function importLoopFile(file) {
  if (!file) return;
  try {
    const raw = await file.text();
    const saved = await request('/api/loops/import', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: raw
    });
    await loadLoops(false);
    await openLoop(saved.id);
    toast(`已导入 ${saved.name}`);
  } catch (error) {
    toast(error.message, true);
  } finally {
    $('#loop-import-file').value = '';
  }
}

async function runLoopDryRun() {
  if (!selectedLoop) return;
  try {
    $('#loop-dry-run').disabled = true;
    loopDryRunReport = await request(`/api/loops/${encodeURIComponent(selectedLoop.id)}/dry-run`, {method: 'POST'});
    renderDryRunReport();
  } catch (error) {
    toast(error.message, true);
  } finally {
    $('#loop-dry-run').disabled = false;
  }
}

function renderDryRunReport() {
  const root = $('#loop-dry-run-result');
  if (!loopDryRunReport) {
    root.className = 'review-empty';
    root.textContent = '运行 Dry Run 检查 readiness。';
    $('#loop-start').disabled = true;
    return;
  }
  const report = loopDryRunReport;
  root.className = 'readiness';
  root.replaceChildren();
  const banner = document.createElement('div');
  banner.className = `readiness-banner ${report.ready ? '' : 'blocked'}`;
  banner.textContent = report.ready ? `Ready · revision ${report.definition_revision}` : `Not ready · ${report.reasons.length} 个阻塞项`;
  root.append(banner);
  const grid = document.createElement('div');
  grid.className = 'readiness-grid';
  const items = [
    ['Workspace', report.workspace_matches ? '匹配' : '不匹配'],
    ['Git', report.git_clean ? 'Clean' : 'Dirty / unavailable'],
    ['Scope', report.write_scope],
    ['Approval', report.requires_approval ? 'Required' : 'Not required'],
    ['Agent', `${report.agent.model} · ${report.agent.agent}`],
    ['Budget', `${report.budget.max_model_calls} calls · ${report.budget.max_tokens} tokens`]
  ];
  for (const [label, value] of items) {
    const card = document.createElement('div');
    card.className = 'readiness-item';
    card.innerHTML = '<span></span><strong></strong>';
    card.querySelector('span').textContent = label;
    card.querySelector('strong').textContent = value || '—';
    grid.append(card);
  }
  root.append(grid);
  if ((report.roles || []).length) {
    const roles = document.createElement('div');
    roles.className = 'team-role-preview';
    for (const role of report.roles) {
      const card = document.createElement('div');
      card.className = 'role-preview';
      card.innerHTML = '<strong></strong><span></span>';
      card.querySelector('strong').textContent = role.role || 'Agent';
      const employee = role.employee_id ? ` · ${role.employee_id} r${role.employee_revision || '?'}` : '';
      card.querySelector('span').textContent = `${role.model || 'unresolved model'}${employee} · ${role.credential_configured ? 'ready' : (role.detail || 'not ready')}`;
      roles.append(card);
    }
    root.append(roles);
  }
  if ((report.reasons || []).length) {
    const list = document.createElement('ul');
    list.className = 'readiness-reasons';
    for (const reason of report.reasons) {
      const item = document.createElement('li');
      item.textContent = reason;
      list.append(item);
    }
    root.append(list);
  }
  $('#loop-start').disabled = !report.ready;
}

async function startLoopInvocation() {
  if (!selectedLoop || !loopDryRunReport || !loopDryRunReport.ready) return;
  try {
    $('#loop-start').disabled = true;
    const invocation = await request(`/api/loops/${encodeURIComponent(selectedLoop.id)}/invocations`, {method: 'POST'});
    loopInvocations = [invocation, ...loopInvocations.filter(item => item.id !== invocation.id)];
    loopInvocationCache.set(selectedLoop.id, loopInvocations);
    renderLoopHistory();
    renderLoopDashboard();
    await openLoopInvocation(invocation.id);
    toast('Invocation 已启动');
  } catch (error) {
    toast(error.message, true);
    await openLoop(selectedLoop.id);
  }
}

async function openLoopInvocation(id) {
  closeLoopEvents();
  selectedLoopInvocation = await request(`/api/loop-invocations/${encodeURIComponent(id)}`);
  localStorage.setItem('gohermit.loop.invocation', id);
  loopSessionState = null;
  loopRuntimeEvents = [];
  if (selectedLoopInvocation.session_id) {
    try {
      loopSessionState = await request(`/api/sessions/${encodeURIComponent(selectedLoopInvocation.session_id)}`);
    } catch (_) {
      loopSessionState = null;
    }
  }
  showLoopTimeline();
  renderLoopTimeline();
  renderLoopHistory();
  if (selectedLoopInvocation.session_id && !terminalInvocationStatuses.has(selectedLoopInvocation.status)) {
    connectLoopEvents(selectedLoopInvocation.session_id, id);
  }
}

function showLoopDefinition() {
  $('#loop-form').classList.toggle('hidden', !selectedLoop && $('#loop-id').value === '');
  $('#loop-timeline-panel').classList.add('hidden');
  $('#loop-definition-tab').classList.add('active');
  $('#loop-timeline-tab').classList.remove('active');
  $('#loop-timeline-tab').disabled = !selectedLoopInvocation;
}

function showLoopTimeline() {
  $('#loop-empty').classList.add('hidden');
  $('#loop-form').classList.add('hidden');
  $('#loop-timeline-panel').classList.remove('hidden');
  $('#loop-definition-tab').classList.remove('active');
  $('#loop-timeline-tab').classList.add('active');
  $('#loop-timeline-tab').disabled = false;
}

function invocationStatusLabel(status) {
  return ({
    prepared: 'Prepared', dispatched: 'Dispatched', attached: 'Running',
    completed: 'Completed', skipped: 'Skipped', blocked: 'Blocked',
    failed: 'Failed', cancelled: 'Cancelled'
  })[status] || status;
}

function renderLoopHistory() {
  const root = $('#loop-history');
  root.replaceChildren();
  if (!loopInvocations.length) {
    const empty = document.createElement('div');
    empty.className = 'review-empty';
    empty.textContent = selectedLoop ? '还没有 Invocation。' : '选择一个 Loop 查看历史。';
    root.append(empty);
    return;
  }
  for (const invocation of loopInvocations) {
    const button = document.createElement('button');
    button.className = `history-item ${selectedLoopInvocation && selectedLoopInvocation.id === invocation.id ? 'active' : ''}`;
    button.innerHTML = '<strong><span class="history-status"></span><span class="history-id"></span></strong><span class="history-time"></span>';
    button.querySelector('.history-status').textContent = invocationStatusLabel(invocation.status);
    button.querySelector('.history-id').textContent = invocation.id.slice(0, 12);
    button.querySelector('.history-time').textContent = `${relativeTime(invocation.created_at)} · r${invocation.definition_revision}`;
    button.addEventListener('click', () => openLoopInvocation(invocation.id));
    root.append(button);
  }
}

function clipped(value, limit = 280) {
  const text = String(value || '').trim();
  return text.length > limit ? `${text.slice(0, limit)}…` : text;
}

function addTimelineEvent(events, title, detail, time, state = '') {
  events.push({title, detail: clipped(detail), time, state});
}

function renderLoopTimeline() {
  const invocation = selectedLoopInvocation;
  if (!invocation) return;
  $('#timeline-title').textContent = `${selectedLoop ? selectedLoop.name : invocation.loop_id} · ${invocationStatusLabel(invocation.status)}`;
  $('#timeline-meta').textContent = `${invocation.id} · definition revision ${invocation.definition_revision}`;
  $('#timeline-open-agent').classList.toggle('hidden', !invocation.session_id);
  const summary = $('#timeline-summary');
  summary.replaceChildren();
  const summaryItems = [
    ['Definition snapshot', `revision ${invocation.definition_revision}`],
    ['Session / Run', invocation.session_id ? `${invocation.session_id.slice(0, 10)} / ${(invocation.run_id || 'pending').slice(0, 10)}` : 'Not created'],
    ['Status', invocationStatusLabel(invocation.status)]
  ];
  for (const [label, value] of summaryItems) {
    const card = document.createElement('article');
    card.innerHTML = '<span></span><strong></strong>';
    card.querySelector('span').textContent = label;
    card.querySelector('strong').textContent = value;
    summary.append(card);
  }

  const events = [];
  addTimelineEvent(events, 'Invocation created', `Manual trigger · snapshot revision ${invocation.definition_revision}`, invocation.created_at);
  if (invocation.failure_code) addTimelineEvent(events, 'Readiness blocked', `${invocation.failure_code}: ${invocation.failure_summary || ''}`, invocation.finished_at, 'error');
  if (invocation.session_id) addTimelineEvent(events, 'Session / Run binding', `${invocation.session_id} · ${invocation.run_id || 'run pending'}`, invocation.started_at, 'success');

  const session = loopSessionState && loopSessionState.session;
  if (session) {
    const run = (session.runs || []).find(item => item.id === invocation.run_id) || (session.runs || []).at(-1);
    if (run && run.plan) {
      for (const step of run.plan.steps || []) {
        addTimelineEvent(events, `Plan · ${step.title}`, step.detail || step.status, run.updated_at, step.status === 'completed' ? 'success' : (step.status === 'failed' ? 'error' : 'warning'));
      }
    }
    if (session.mission) {
      for (const item of session.mission.work_items || []) {
        addTimelineEvent(events, `${roleLabel(item.role)} · ${item.title}`, item.status, item.completed_at || item.started_at, item.status === 'completed' ? 'success' : (item.status === 'failed' ? 'error' : ''));
      }
    }
    for (const tool of (session.tool_calls || []).filter(item => !item.run_id || item.run_id === invocation.run_id)) {
      addTimelineEvent(events, `Tool · ${tool.name}`, tool.summary || tool.status, tool.completed_at || tool.started_at || tool.time, tool.is_error ? 'error' : 'success');
    }
    for (const approval of (session.approval_requests || []).filter(item => !item.run_id || item.run_id === invocation.run_id)) {
      addTimelineEvent(events, `Approval · ${approval.tool}`, `${approval.status} · ${(approval.resource_paths || []).join(', ')}`, approval.created_at, approval.status === 'approved' || approval.status === 'consumed' ? 'success' : 'warning');
    }
    for (const check of (session.test_results || []).filter(item => !item.run_id || item.run_id === invocation.run_id)) {
      addTimelineEvent(events, `Verification · ${check.command}`, check.summary || (check.passed ? 'passed' : 'failed'), check.time, check.passed ? 'success' : 'error');
    }
    if (run && run.verification_attempts > 1) addTimelineEvent(events, 'Repair / reverify', `${run.verification_attempts} verification attempts`, run.updated_at, 'warning');
    if (run && run.final_message) addTimelineEvent(events, 'Owner summary', run.final_message, run.completed_at, run.status === 'completed' ? 'success' : '');
    else if (session.summary) addTimelineEvent(events, 'Owner summary', session.summary, session.updated_at, '');
  }
  for (const runtimeEvent of loopRuntimeEvents) {
    const detail = runtimeEvent.error || runtimeEvent.message || runtimeEvent.tool || '';
    addTimelineEvent(events, eventLabel(runtimeEvent.type), detail, runtimeEvent.time, runtimeEvent.type.includes('failed') || runtimeEvent.type.includes('cancelled') ? 'error' : '');
  }
  if (terminalInvocationStatuses.has(invocation.status)) {
    addTimelineEvent(events, invocationStatusLabel(invocation.status), invocation.failure_summary || 'Invocation reached a terminal state.', invocation.finished_at, invocation.status === 'completed' ? 'success' : 'error');
  }
  const root = $('#timeline-events');
  root.replaceChildren();
  for (const item of events) {
    const row = document.createElement('li');
    row.className = `timeline-event ${item.state}`;
    row.innerHTML = '<strong></strong><span></span><small></small>';
    row.querySelector('strong').textContent = item.title;
    row.querySelector('span').textContent = item.detail || '—';
    row.querySelector('small').textContent = item.time ? new Date(item.time).toLocaleString() : '';
    root.append(row);
  }
  const active = !terminalInvocationStatuses.has(invocation.status);
  $('#loop-cancel').classList.toggle('hidden', !active);
  $('#loop-start').disabled = !loopDryRunReport || !loopDryRunReport.ready || active;
}

function connectLoopEvents(sessionID, invocationID) {
  closeLoopEvents();
  const sequenceKey = `gohermit.loop.sequence.${invocationID}`;
  const after = Number(localStorage.getItem(sequenceKey) || 0);
  loopEventStream = new EventSource(`/api/sessions/${encodeURIComponent(sessionID)}/events?after=${after}`);
  for (const type of loopEventTypes) {
    loopEventStream.addEventListener(type, source => {
      let runtimeEvent;
      try {
        runtimeEvent = JSON.parse(source.data);
      } catch (_) {
        return;
      }
      runtimeEvent.type = type;
      if (runtimeEvent.sequence) localStorage.setItem(sequenceKey, String(runtimeEvent.sequence));
      loopRuntimeEvents.push(runtimeEvent);
      if (loopRuntimeEvents.length > 100) loopRuntimeEvents.shift();
      renderLoopTimeline();
      if (['session_updated', 'plan_updated', 'approval_requested', 'approval_decided', 'task_completed', 'task_failed', 'task_cancelled'].includes(type)) {
        setTimeout(refreshLoopInvocation, 100);
      }
    });
  }
}

function closeLoopEvents() {
  if (loopEventStream) loopEventStream.close();
  loopEventStream = null;
}

async function refreshLoopInvocation() {
  if (!selectedLoopInvocation) return;
  const id = selectedLoopInvocation.id;
  selectedLoopInvocation = await request(`/api/loop-invocations/${encodeURIComponent(id)}`);
  if (selectedLoopInvocation.session_id) {
    loopSessionState = await request(`/api/sessions/${encodeURIComponent(selectedLoopInvocation.session_id)}`);
  }
  loopInvocations = loopInvocations.map(item => item.id === id ? selectedLoopInvocation : item);
  loopInvocationCache.set(selectedLoopInvocation.loop_id, loopInvocations);
  renderLoopHistory();
  renderLoopTimeline();
  renderLoopDashboard();
  if (terminalInvocationStatuses.has(selectedLoopInvocation.status)) closeLoopEvents();
}

async function cancelLoopInvocation() {
  if (!selectedLoopInvocation || terminalInvocationStatuses.has(selectedLoopInvocation.status)) return;
  try {
    selectedLoopInvocation = await request(`/api/loop-invocations/${encodeURIComponent(selectedLoopInvocation.id)}/cancel`, {method: 'POST'});
    await refreshLoopInvocation();
    toast('Invocation 已取消');
  } catch (error) {
    toast(error.message, true);
  }
}

function openInvocationInAgent() {
  if (!selectedLoopInvocation || !selectedLoopInvocation.session_id) return;
  switchWorkbenchView('agent');
  openSession(selectedLoopInvocation.session_id).catch(error => toast(error.message, true));
}

async function renderTeamRolePreview() {
  const root = $('#loop-team-roles');
  root.replaceChildren();
  if ($('#loop-agent').value !== 'team') return;
  if (!loopTeamTemplate) {
    try {
      loopTeamTemplate = await request('/api/team-template/export');
    } catch (_) {
      loopTeamTemplate = {};
    }
  }
  if (!loopEmployees.length) {
    try {
      const page = await request('/api/employees?limit=100&state=active');
      loopEmployees = page.employees || [];
    } catch (_) {
      loopEmployees = [];
    }
  }
  const roles = ['explorer', 'builder', 'reviewer', 'verifier', 'lead'];
  const fallback = loopTeamTemplate.default || collectLoopDefinition().agent_selection;
  for (const role of roles) {
    const selection = (loopTeamTemplate.roles && loopTeamTemplate.roles[role]) || fallback;
    const card = document.createElement('div');
    card.className = 'role-preview';
    card.innerHTML = '<strong></strong><span></span><label>Employee<select data-testid="team-role-employee"><option value="">No Employee (legacy Role)</option></select></label><label class="check-required"><input type="checkbox" data-testid="team-role-default-model"><span>Use Employee default model</span></label><small></small>';
    card.querySelector('strong').textContent = roleLabel(role);
    const employee = loopEmployees.find(item => item.id === (selection && selection.employee_id));
    card.querySelector('span').textContent = selection && selection.model
      ? `${selection.model} · ${selection.access || ''}${selection.employee_id ? ' · Mission override' : ''}`
      : (selection && selection.employee_id ? 'Employee default model' : 'uses loop selection');
    const select = card.querySelector('select');
    for (const item of loopEmployees) {
      const option = document.createElement('option');
      option.value = item.id;
      option.textContent = `${item.name} · r${item.revision} · ${item.state}`;
      select.append(option);
    }
    if (selection && selection.employee_id && !employee) {
      const stale = document.createElement('option');
      stale.value = selection.employee_id;
      stale.textContent = `${selection.employee_id} · unavailable`;
      select.append(stale);
    }
    select.value = (selection && selection.employee_id) || '';
    const defaultModel = card.querySelector('input');
    defaultModel.checked = Boolean(selection && selection.employee_id && !selection.model);
    defaultModel.disabled = !select.value;
    card.querySelector('small').textContent = employee
      ? `Pinned at Mission preflight: ${employee.id} r${employee.revision}. Run Dry Run for live readiness.`
      : (selection && selection.employee_id ? 'Employee is unavailable; preflight will fail closed.' : 'Legacy RoleSelection behavior is unchanged.');
    const save = () => saveTeamRoleEmployee(role, select.value, defaultModel.checked).catch(error => {
      toast(error.message || String(error));
    });
    select.addEventListener('change', () => {
      defaultModel.disabled = !select.value;
      save();
    });
    defaultModel.addEventListener('change', save);
    root.append(card);
  }
}

async function saveTeamRoleEmployee(role, employeeID, useEmployeeDefault) {
  const next = structuredClone(loopTeamTemplate || {});
  next.schema_version = 2;
  next.name = next.name || 'default';
  next.default = next.default || {
    company: $('#loop-company').value,
    access: $('#loop-access').value,
    model: $('#loop-model').value,
  };
  next.roles = next.roles || {};
  const current = structuredClone(next.roles[role] || next.default);
  if (employeeID) {
    current.employee_id = employeeID;
    if (useEmployeeDefault) {
      current.company = '';
      current.access = '';
      current.model = '';
    } else if (!current.model) {
      current.company = next.default.company || $('#loop-company').value;
      current.access = next.default.access || $('#loop-access').value;
      current.model = next.default.model || $('#loop-model').value;
    }
  } else {
    delete current.employee_id;
    if (!current.model) {
      current.company = next.default.company || $('#loop-company').value;
      current.access = next.default.access || $('#loop-access').value;
      current.model = next.default.model || $('#loop-model').value;
    }
  }
  next.roles[role] = current;
  await request('/api/team-template/import', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify(next),
  });
  loopTeamTemplate = next;
  toast(employeeID ? `${roleLabel(role)} assigned to ${employeeID}` : `${roleLabel(role)} restored to legacy Role selection`);
  await renderTeamRolePreview();
}

function renderLoopDashboard() {
  $('#dashboard-loop-count').textContent = String(loopDefinitions.length);
  const all = [...loopInvocationCache.values()].flat().sort((a, b) => new Date(b.created_at) - new Date(a.created_at));
  $('#dashboard-active-count').textContent = String(all.filter(item => !terminalInvocationStatuses.has(item.status)).length);
  $('#dashboard-blocked-count').textContent = String(all.filter(item => item.status === 'blocked' || item.failure_code === 'waiting_owner').length);
  $('#dashboard-failed-count').textContent = String(all.filter(item => item.status === 'failed').length);
  const root = $('#dashboard-invocations');
  root.replaceChildren();
  if (!all.length) {
    const empty = document.createElement('div');
    empty.className = 'review-empty';
    empty.textContent = '还没有 Invocation。进入 Loop Workbench 创建第一个可恢复工作流。';
    root.append(empty);
    return;
  }
  for (const invocation of all.slice(0, 10)) {
    const definition = loopDefinitions.find(item => item.id === invocation.loop_id);
    const button = document.createElement('button');
    button.className = 'dashboard-invocation';
    button.innerHTML = '<div><strong></strong><span></span></div><em></em>';
    button.querySelector('strong').textContent = definition ? definition.name : invocation.loop_id;
    button.querySelector('span').textContent = `${relativeTime(invocation.created_at)} · revision ${invocation.definition_revision}${invocation.failure_code ? ` · ${invocation.failure_code}` : ''}`;
    button.querySelector('em').textContent = invocationStatusLabel(invocation.status);
    button.addEventListener('click', async () => {
      switchWorkbenchView('loops');
      await openLoop(invocation.loop_id);
      await openLoopInvocation(invocation.id);
    });
    root.append(button);
  }
}

function refillLoopAccess() {
  const companies = (catalog && catalog.available_companies) || [];
  const company = companies.find(item => item.id === $('#loop-company').value);
  setOptions($('#loop-access'), company ? company.access : [], '', item => item.label);
  refillLoopModels();
}

function refillLoopModels() {
  const companies = (catalog && catalog.available_companies) || [];
  const company = companies.find(item => item.id === $('#loop-company').value);
  const access = company && company.access.find(item => item.id === $('#loop-access').value);
  setOptions($('#loop-model'), access ? access.models : [], '', item => item.label);
  renderTeamRolePreview();
  updateLoopJSONPreview();
}

$('#dashboard-button').addEventListener('click', () => switchWorkbenchView('dashboard'));
$('#employees-button').addEventListener('click', () => switchWorkbenchView('employees'));
$('#employee-tasks-button').addEventListener('click', () => switchWorkbenchView('employee-tasks'));
$('#tasks-button').addEventListener('click', () => switchWorkbenchView('agent'));
$('#loops-button').addEventListener('click', () => switchWorkbenchView('loops'));
$('#brand-button').addEventListener('click', () => switchWorkbenchView('agent'));
$('#dashboard-open-loops').addEventListener('click', () => switchWorkbenchView('loops'));
$('#loop-new-button').addEventListener('click', newLoop);
$('#loop-empty-new').addEventListener('click', newLoop);
$('#loop-search').addEventListener('input', renderLoopList);
$('#loop-import-button').addEventListener('click', () => $('#loop-import-file').click());
$('#loop-import-file').addEventListener('change', event => importLoopFile(event.target.files[0]));
$('#loop-form').addEventListener('submit', saveLoop);
$('#loop-form').addEventListener('input', updateLoopJSONPreview);
$('#loop-add-check').addEventListener('click', () => {
  addVerificationCheck();
  updateLoopJSONPreview();
});
$('#loop-company').addEventListener('change', refillLoopAccess);
$('#loop-access').addEventListener('change', refillLoopModels);
$('#loop-agent').addEventListener('change', () => {
  renderTeamRolePreview();
  updateLoopJSONPreview();
});
$('#loop-read-only').addEventListener('change', () => {
  if ($('#loop-read-only').checked) $('#loop-approval').checked = false;
  updateLoopJSONPreview();
});
$('#loop-dry-run').addEventListener('click', runLoopDryRun);
$('#loop-start').addEventListener('click', startLoopInvocation);
$('#loop-cancel').addEventListener('click', cancelLoopInvocation);
$('#timeline-open-agent').addEventListener('click', openInvocationInAgent);
$('#loop-definition-tab').addEventListener('click', showLoopDefinition);
$('#loop-timeline-tab').addEventListener('click', showLoopTimeline);
document.addEventListener('gohermit:catalog', () => {
  if (selectedLoop) fillLoopSelects(selectedLoop);
  renderTeamRolePreview();
  updateLoopJSONPreview();
});

(async function bootLoopWorkbench() {
  switchWorkbenchView(localStorage.getItem('gohermit.view') || 'agent');
  try {
    await loadLoops();
  } catch (_) {
    renderLoopDashboard();
  }
})();
