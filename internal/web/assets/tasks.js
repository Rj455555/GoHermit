let employeeTaskItems = [];
let selectedEmployeeTask = null;
let selectedTaskSession = null;
let selectedTaskMessages = [];
let taskEmployees = [];
let taskCreateContext = null;
const taskEventStreams = new Map();
let activeTaskEventKey = '';
let taskCheckpointRefresh = null;

const employeeTaskTerminalStates = new Set(['completed', 'failed', 'cancelled']);
const taskSessionEventTypes = [
  'task_started', 'turn_started', 'model_started', 'model_completed',
  'tool_started', 'tool_completed', 'permission_required', 'checkpoint_saved',
  'run_verifying', 'run_interrupted', 'workspace_changed', 'session_updated',
  'plan_created', 'plan_updated', 'approval_requested', 'approval_decided',
  'approval_expired', 'approval_consumed', 'task_completed', 'task_failed',
  'task_cancelled',
];

function showTaskError(error) {
  const panel = $('#task-error');
  if (panel) {
    panel.textContent = error && error.message ? error.message : String(error);
    panel.classList.remove('hidden');
  }
  toast(error && error.message ? error.message : String(error), true);
}

function clearTaskError() {
  $('#task-error').textContent = '';
  $('#task-error').classList.add('hidden');
}

async function loadEmployeeTaskWorkbench(openSaved = true) {
  const page = await request('/api/employees?limit=100');
  taskEmployees = page.employees || [];
  fillTaskEmployeeFilters();
  const pages = await Promise.all(taskEmployees.map(async value => {
    const data = await request(`/api/employees/${encodeURIComponent(value.id)}/tasks?limit=100`);
    return data.tasks || [];
  }));
  employeeTaskItems = pages.flat().sort((a, b) => String(b.created_at).localeCompare(String(a.created_at)));
  fillTaskProjectFilters();
  renderEmployeeTaskList();
  if (!openSaved) return;
  const savedID = localStorage.getItem('gohermit.employee-task');
  if (savedID && employeeTaskItems.some(item => item.id === savedID)) await openEmployeeTask(savedID);
}

function fillTaskProjectFilters() {
  const previous = $('#task-project-filter').value;
  const projects = new Map();
  for (const task of employeeTaskItems) {
    if (task.project_binding && task.project_binding.id) {
      projects.set(task.project_binding.id, task.project_binding.label || task.project_binding.id);
    }
  }
  setOptions(
    $('#task-project-filter'),
    [{id: '', label: 'All Projects'}, ...[...projects].map(([id, label]) => ({id, label}))],
    previous,
    item => item.label,
  );
}

function fillTaskEmployeeFilters() {
  const previousFilter = $('#task-employee-filter').value;
  const previousCreate = $('#task-create-employee').value;
  const filterItems = [{id: '', name: 'All Employees'}, ...taskEmployees];
  setOptions($('#task-employee-filter'), filterItems, previousFilter, item => item.name || item.id);
  setOptions($('#task-create-employee'), taskEmployees.filter(item => item.state === 'active'), previousCreate, item => item.name);
}

function renderEmployeeTaskList() {
  const root = $('#employee-task-list');
  root.replaceChildren();
  const employeeID = $('#task-employee-filter').value;
  const projectID = $('#task-project-filter').value;
  const state = $('#task-state-filter').value;
  const updatedWindow = Number($('#task-updated-filter').value || 0);
  const cutoff = updatedWindow > 0 ? Date.now() - updatedWindow * 1000 : 0;
  const visible = employeeTaskItems.filter(item =>
    (!employeeID || item.employee_id === employeeID) &&
    (!projectID || (item.project_binding && item.project_binding.id === projectID)) &&
    (!state || item.state === state) &&
    (!cutoff || Date.parse(item.updated_at) >= cutoff));
  if (!visible.length) {
    const empty = document.createElement('div');
    empty.className = 'sidebar-empty';
    empty.textContent = 'No matching Tasks';
    root.append(empty);
    return;
  }
  for (const task of visible) {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = `resource-list-item ${selectedEmployeeTask && selectedEmployeeTask.id === task.id ? 'active' : ''}`;
    button.dataset.testid = 'task-row';
    button.innerHTML = '<i class="task-state-dot"></i><div><strong></strong><span></span><small></small></div>';
    button.querySelector('i').classList.add(task.state);
    button.querySelector('strong').textContent = task.prompt;
    button.querySelector('span').textContent = taskEmployees.find(item => item.id === task.employee_id)?.name || task.employee_id;
    button.querySelector('small').textContent = `${task.state} · ${relativeTime(task.updated_at)}`;
    button.addEventListener('click', () => openEmployeeTask(task.id));
    root.append(button);
  }
}

function showTaskEmpty() {
  closeTaskEvents();
  $('#task-empty').classList.remove('hidden');
  $('#task-create-panel').classList.add('hidden');
  $('#task-detail').classList.add('hidden');
}

async function startTaskCreate() {
  if (!taskEmployees.length) {
    try { await loadEmployeeTaskWorkbench(false); } catch (error) { showTaskError(error); return; }
  }
  const active = taskEmployees.filter(item => item.state === 'active');
  if (!active.length) {
    showTaskError(new Error('Create or enable an Employee before creating a Task.'));
    return;
  }
  $('#task-create-panel').reset();
  $('#task-capabilities').value = 'read';
  $('#task-max-calls').value = '4';
  $('#task-max-tokens').value = '4000';
  $('#task-timeout').value = '600';
  $('#task-create-error').classList.add('hidden');
  $('#task-empty').classList.add('hidden');
  $('#task-detail').classList.add('hidden');
  $('#task-create-panel').classList.remove('hidden');
  await loadTaskCreateContext($('#task-create-employee').value || active[0].id);
  $('#task-prompt').focus();
}

async function loadTaskCreateContext(employeeID) {
  const [record, skills, knowledge, memory] = await Promise.all([
    request(`/api/employees/${encodeURIComponent(employeeID)}`),
    request(`/api/employees/${encodeURIComponent(employeeID)}/skills`),
    request(`/api/employees/${encodeURIComponent(employeeID)}/knowledge?limit=32`),
    request(`/api/employees/${encodeURIComponent(employeeID)}/memory`),
  ]);
  taskCreateContext = {record, skills, knowledge, memory};
  setOptions($('#task-project'), record.project_bindings || [], '', item => item.label);
  renderTaskCreateSelections();
}

function renderTaskCreateSelections() {
  const skillRoot = $('#task-skill-selection');
  const knowledgeRoot = $('#task-knowledge-selection');
  const memoryRoot = $('#task-memory-selection');
  skillRoot.replaceChildren();
  knowledgeRoot.replaceChildren();
  memoryRoot.replaceChildren();
  for (const item of taskCreateContext.skills.bindings || []) {
    if (!item.binding.enabled || item.status !== 'current') continue;
    const selection = taskSelectionItem(
      item.binding.skill_id,
      `${item.binding.version} · ${item.binding.digest} · ${item.status}`,
      `${item.binding.skill_id}\0${item.binding.version}\0${item.binding.digest}`,
      'skill',
      true,
    );
    const input = selection.querySelector('input');
    input.dataset.skillID = item.binding.skill_id;
    input.dataset.version = item.binding.version;
    input.dataset.digest = item.binding.digest;
    skillRoot.append(selection);
  }
  const citationsBySource = new Map();
  for (const index of taskCreateContext.knowledge.indexes || []) {
    citationsBySource.set(index.source_id, (index.documents || []).flatMap(document => document.citations || []));
  }
  for (const source of taskCreateContext.knowledge.sources || []) {
    const citations = citationsBySource.get(source.id) || (taskCreateContext.knowledge.results || []).filter(item => item.source_id === source.id).map(item => item.citation);
    const item = taskSelectionItem(source.title, `${citations.length} bounded citations`, source.id, 'knowledge', false);
    item.querySelector('input').dataset.citations = JSON.stringify(citations.map(value => value.id));
    knowledgeRoot.append(item);
  }
  for (const fact of taskCreateContext.memory.facts || []) {
    memoryRoot.append(taskSelectionItem(fact.category, fact.value, fact.id, 'memory', false));
  }
  for (const [root, text] of [[skillRoot, 'No current Skill bindings'], [knowledgeRoot, 'No Knowledge sources'], [memoryRoot, 'No accepted Memory facts']]) {
    if (!root.childElementCount) {
      const empty = document.createElement('div');
      empty.className = 'review-empty';
      empty.textContent = text;
      root.append(empty);
    }
  }
}

function taskSelectionItem(title, detail, id, kind, checked) {
  const label = document.createElement('label');
  label.className = 'selection-item';
  label.innerHTML = '<input type="checkbox"><div><strong></strong><span></span></div>';
  const input = label.querySelector('input');
  input.dataset.id = id;
  input.dataset.kind = kind;
  input.checked = checked;
  label.querySelector('strong').textContent = title;
  label.querySelector('span').textContent = detail;
  return label;
}

function selectedTaskCreateValues(kind) {
  return [...$('#task-create-panel').querySelectorAll(`input[data-kind="${kind}"]:checked`)];
}

function taskCreatePayload() {
  return {
    prompt: $('#task-prompt').value,
    skills: selectedTaskCreateValues('skill').map(input => {
      const item = taskCreateContext.skills.bindings.find(value =>
        value.binding.skill_id === input.dataset.skillID &&
        value.binding.version === input.dataset.version &&
        value.binding.digest === input.dataset.digest);
      return {skill_id: item.binding.skill_id, version: item.binding.version};
    }),
    knowledge: selectedTaskCreateValues('knowledge').map(input => ({
      source_id: input.dataset.id,
      citation_ids: JSON.parse(input.dataset.citations || '[]'),
    })),
    memory_fact_ids: selectedTaskCreateValues('memory').map(input => input.dataset.id),
    project_binding_id: $('#task-project').value,
    policy: {
      allowed_capabilities: $('#task-capabilities').value.split(',').map(item => item.trim()).filter(Boolean),
      network_allowed: $('#task-network').checked,
      budget: {
        max_model_calls: Number($('#task-max-calls').value),
        max_tokens: Number($('#task-max-tokens').value),
        timeout_seconds: Number($('#task-timeout').value),
      },
    },
  };
}

async function createEmployeeTask(event) {
  event.preventDefault();
  const errorPanel = $('#task-create-error');
  errorPanel.classList.add('hidden');
  if (!$('#task-prompt').reportValidity()) return;
  try {
    const employeeID = $('#task-create-employee').value;
    const task = await request(`/api/employees/${encodeURIComponent(employeeID)}/tasks`, {
      method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(taskCreatePayload()),
    });
    employeeTaskItems = [task, ...employeeTaskItems.filter(item => item.id !== task.id)];
    renderEmployeeTaskList();
    await openEmployeeTask(task.id, task);
    toast('Task queued. Start remains explicit.');
  } catch (error) {
    errorPanel.textContent = error.message;
    errorPanel.classList.remove('hidden');
  }
}

async function openEmployeeTask(taskID, knownTask = null) {
  clearTaskError();
  let task;
  try {
    task = knownTask || await request(`/api/employee-tasks/${encodeURIComponent(taskID)}`);
  } catch (error) {
    showTaskError(error);
    return;
  }
  let sessionState = null;
  let approvals = [];
  if (task.session_id) {
    try {
      const [sessionData, approvalData] = await Promise.all([
        request(`/api/sessions/${encodeURIComponent(task.session_id)}`),
        request(`/api/sessions/${encodeURIComponent(task.session_id)}/approvals`).catch(() => ({approvals: []})),
      ]);
      sessionState = sessionData.session;
      selectedTaskMessages = sessionData.messages || [];
      approvals = approvalData.approvals || [];
    } catch (error) {
      showTaskError(error);
      if (!selectedEmployeeTask || selectedEmployeeTask.id !== taskID) return;
      task = selectedEmployeeTask;
      sessionState = selectedTaskSession;
      approvals = selectedTaskSession?._employeeApprovals || [];
    }
  }
  selectedEmployeeTask = task;
  selectedTaskSession = sessionState;
  if (selectedTaskSession) selectedTaskSession._employeeApprovals = approvals;
  localStorage.setItem('gohermit.employee-task', task.id);
  employeeTaskItems = [task, ...employeeTaskItems.filter(item => item.id !== task.id)];
  $('#task-empty').classList.add('hidden');
  $('#task-create-panel').classList.add('hidden');
  $('#task-detail').classList.remove('hidden');
  renderEmployeeTaskList();
  renderEmployeeTaskDetail();
  if (task.session_id) connectTaskEvents(task, false);
  else closeTaskEvents();
}

function renderEmployeeTaskDetail() {
  const task = selectedEmployeeTask;
  const employeeName = taskEmployees.find(item => item.id === task.employee_id)?.name || task.employee_id;
  $('#task-detail-employee').textContent = employeeName;
  $('#task-detail-title').textContent = task.prompt;
  $('#task-detail-meta').textContent = `${task.id} · Employee revision ${task.employee_revision} · ${task.session_id || 'not started'}`;
  $('#task-status').textContent = task.state;
  $('#task-status').className = `status-pill ${task.state}`;
  $('#task-start').classList.toggle('hidden', task.state !== 'queued' || Boolean(task.run_id));
  $('#task-resume').classList.toggle('hidden', task.state !== 'interrupted');
  $('#task-cancel').classList.toggle('hidden', employeeTaskTerminalStates.has(task.state));
  $('#task-open-agent').classList.toggle('hidden', !task.session_id);
  renderTaskPlan();
  renderTaskTools();
  renderTaskVerification();
  renderTaskApprovals();
  renderTaskContext();
  renderTaskArtifacts();
  renderTaskRuntimeEvents();
}

function selectedTaskRun() {
  if (!selectedTaskSession) return null;
  return (selectedTaskSession.runs || []).find(run => run.id === selectedEmployeeTask.run_id) ||
    (selectedTaskSession.runs || []).at(-1) || null;
}

function renderTaskPlan() {
  const root = $('#task-plan');
  root.replaceChildren();
  const run = selectedTaskRun();
  const plan = (run && run.plan) || (selectedTaskSession && selectedTaskSession.plan);
  for (const step of (plan && plan.steps) || []) {
    const row = document.createElement('li');
    row.dataset.testid = 'task-plan-step';
    row.innerHTML = '<input type="checkbox" disabled><span></span><em class="status-pill"></em>';
    row.querySelector('input').checked = step.status === 'completed';
    row.querySelector('span').textContent = step.title || step.description || step.id;
    row.querySelector('em').textContent = step.status;
    root.append(row);
  }
  if (!root.childElementCount) root.innerHTML = '<li class="timeline-empty">No Plan has been persisted for this Run.</li>';
}

function renderTaskTools() {
  const root = $('#task-tools');
  root.replaceChildren();
  for (const tool of (selectedTaskSession && selectedTaskSession.tool_calls) || (selectedTaskSession && selectedTaskSession.tools) || []) {
    if (selectedEmployeeTask.run_id && tool.run_id && tool.run_id !== selectedEmployeeTask.run_id) continue;
    const row = document.createElement('div');
    row.className = 'task-tool';
    row.dataset.testid = 'task-tool';
    row.innerHTML = '<div><strong></strong><span></span></div><em class="status-pill"></em>';
    row.querySelector('strong').textContent = tool.name;
    row.querySelector('span').textContent = tool.summary || `turn ${tool.turn || 'legacy'} · ${(tool.args_digest || '').slice(0, 12)}`;
    row.querySelector('em').textContent = tool.status || (tool.is_error ? 'failed' : 'completed');
    root.append(row);
  }
  if (!root.childElementCount) root.innerHTML = '<div class="timeline-empty">No Tool calls recorded.</div>';
}

function renderTaskVerification() {
  const root = $('#task-verification');
  const run = selectedTaskRun();
  const explicit = selectedTaskSession && selectedTaskSession.verification;
  const results = (selectedTaskSession && selectedTaskSession.test_results) || [];
  const summary = explicit && explicit.summary
    ? explicit.summary
    : results.length
      ? results.map(result => `${result.passed ? '✓' : '✕'} ${result.command}: ${result.summary}`).join('\n')
      : run && run.status === 'completed'
        ? run.final_message || 'Verification completed.'
        : run && run.status === 'failed'
          ? run.error || 'Verification failed.'
          : 'No verification result yet.';
  root.textContent = summary;
}

function renderTaskApprovals() {
  const root = $('#task-approvals');
  root.replaceChildren();
  const approvals = selectedTaskSession?._employeeApprovals || [];
  for (const approval of approvals) {
    const card = document.createElement('div');
    card.className = 'context-line';
    card.dataset.testid = 'task-approval';
    card.innerHTML = '<strong></strong><span></span><div class="resource-row-actions"><button class="small-button approve">Approve</button><button class="small-button danger-text deny">Deny</button></div>';
    card.querySelector('strong').textContent = `${approval.tool} · ${approval.status}`;
    card.querySelector('span').textContent = `${approval.args_summary || ''} ${(approval.resource_paths || []).join(', ')}`;
    const pending = approval.status === 'pending';
    card.querySelector('.approve').classList.toggle('hidden', !pending);
    card.querySelector('.deny').classList.toggle('hidden', !pending);
    card.querySelector('.approve').addEventListener('click', () => decideTaskApproval(approval.request_id, 'approve'));
    card.querySelector('.deny').addEventListener('click', () => decideTaskApproval(approval.request_id, 'deny'));
    root.append(card);
  }
  if (!root.childElementCount) root.innerHTML = '<div class="review-empty">No pending approvals.</div>';
}

async function decideTaskApproval(requestID, decision) {
  try {
    await request(`/api/sessions/${encodeURIComponent(selectedEmployeeTask.session_id)}/approvals/${encodeURIComponent(requestID)}/decide`, {
      method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({decision}),
    });
    await refreshTaskCheckpoint(false);
  } catch (error) { showTaskError(error); }
}

function renderTaskContext() {
  const root = $('#task-context');
  root.replaceChildren();
  for (const [label, value] of [
    ['Snapshot', selectedEmployeeTask.snapshot_digest],
    ['Project', selectedEmployeeTask.project_binding && selectedEmployeeTask.project_binding.label],
    ['Skills', String((selectedEmployeeTask.skills || []).length)],
    ['Knowledge', String((selectedEmployeeTask.knowledge || []).length)],
    ['Memory facts', String((selectedEmployeeTask.memory_facts || []).length)],
    ['Session / Run', selectedEmployeeTask.session_id ? `${selectedEmployeeTask.session_id} / ${selectedEmployeeTask.run_id}` : 'Not bound'],
  ]) {
    const row = document.createElement('div');
    row.className = 'context-line';
    row.innerHTML = '<strong></strong><span></span>';
    row.querySelector('strong').textContent = label;
    row.querySelector('span').textContent = value || '—';
    root.append(row);
  }
}

function renderTaskArtifacts() {
  const root = $('#task-artifacts');
  root.replaceChildren();
  for (const artifact of selectedEmployeeTask.artifacts || []) {
    const row = document.createElement('div');
    row.className = 'context-line';
    row.innerHTML = '<strong></strong><span></span>';
    row.querySelector('strong').textContent = artifact.label || artifact.kind || artifact.id;
    row.querySelector('span').textContent = artifact.path || artifact.digest || 'bounded metadata';
    root.append(row);
  }
  if (!root.childElementCount) root.innerHTML = '<div class="review-empty">No verified Artifacts.</div>';
}

function taskEventKey(taskID, sessionID) {
  return `${taskID}\0${sessionID}`;
}

function taskEventStorageKey(taskID, sessionID) {
  return `gohermit.task-sse.${taskID}.${sessionID}`;
}

function taskEventStream(task) {
  const key = taskEventKey(task.id, task.session_id);
  let stream = taskEventStreams.get(key);
  if (!stream) {
    const saved = Number(localStorage.getItem(taskEventStorageKey(task.id, task.session_id)) || 0);
    stream = {
      taskID: task.id,
      sessionID: task.session_id,
      runID: task.run_id || '',
      lastSequence: Number.isSafeInteger(saved) && saved > 0 ? saved : 0,
      events: [],
      source: null,
      healthy: false,
    };
    taskEventStreams.set(key, stream);
  }
  stream.runID = task.run_id || '';
  return stream;
}

function connectTaskEvents(task, force) {
  const key = taskEventKey(task.id, task.session_id);
  const stream = taskEventStream(task);
  if (!force && activeTaskEventKey === key && stream.source && stream.source.readyState !== EventSource.CLOSED) return;
  if (activeTaskEventKey && activeTaskEventKey !== key) closeTaskEvents();
  if (stream.source) stream.source.close();
  activeTaskEventKey = key;
  stream.healthy = true;
  stream.source = new EventSource(`/api/sessions/${encodeURIComponent(task.session_id)}/events?after=${stream.lastSequence}`);
  for (const type of taskSessionEventTypes) {
    stream.source.addEventListener(type, event => {
      let data = {};
      try { data = JSON.parse(event.data || '{}'); } catch (_) { return; }
      const sequence = Number(event.lastEventId || data.sequence);
      if (!Number.isSafeInteger(sequence) || sequence <= stream.lastSequence) return;
      const eventRunID = data.run_id || data.runID || '';
      if (stream.runID && eventRunID && eventRunID !== stream.runID) return;
      stream.lastSequence = sequence;
      localStorage.setItem(taskEventStorageKey(stream.taskID, stream.sessionID), String(sequence));
      stream.events.push({type, data, sequence, time: new Date().toISOString()});
      if (stream.events.length > 200) stream.events.shift();
      renderTaskRuntimeEvents();
      if (['session_updated', 'plan_updated', 'approval_decided', 'task_completed', 'task_failed', 'task_cancelled'].includes(type)) {
        refreshTaskCheckpoint(false).catch(error => showTaskError(error));
      }
    });
  }
  stream.source.onerror = () => {
    stream.healthy = false;
    showTaskError(new Error('Live updates disconnected. Existing timeline is preserved; use Refresh to recover.'));
  };
}

function closeTaskEvents() {
  if (!activeTaskEventKey) return;
  const stream = taskEventStreams.get(activeTaskEventKey);
  if (stream && stream.source) stream.source.close();
  if (stream) {
    stream.source = null;
    stream.healthy = false;
  }
  activeTaskEventKey = '';
}

function renderTaskRuntimeEvents() {
  const root = $('#task-events');
  root.replaceChildren();
  const key = selectedEmployeeTask && selectedEmployeeTask.session_id
    ? taskEventKey(selectedEmployeeTask.id, selectedEmployeeTask.session_id)
    : '';
  const stream = key ? taskEventStreams.get(key) : null;
  for (const event of ((stream && stream.events) || []).slice(-50)) {
    const row = document.createElement('li');
    row.className = 'timeline-event';
    row.innerHTML = '<strong></strong><span></span><small></small>';
    row.querySelector('strong').textContent = event.type.replaceAll('_', ' ');
    row.querySelector('span').textContent = event.data.message || event.data.summary || '';
    row.querySelector('small').textContent = new Date(event.time).toLocaleTimeString();
    root.append(row);
  }
  if (!root.childElementCount) root.innerHTML = '<li class="timeline-empty">Waiting for Session events.</li>';
}

async function mutateEmployeeTask(action) {
  clearTaskError();
  try {
    const task = await request(`/api/employee-tasks/${encodeURIComponent(selectedEmployeeTask.id)}/${action}`, {method: 'POST'});
    await openEmployeeTask(task.id, task);
  } catch (error) { showTaskError(error); }
}

async function refreshTaskCheckpoint(recoverEvents) {
  if (!selectedEmployeeTask) return;
  if (taskCheckpointRefresh) return taskCheckpointRefresh;
  const taskID = selectedEmployeeTask.id;
  taskCheckpointRefresh = (async () => {
    await openEmployeeTask(taskID);
    if (!recoverEvents || !selectedEmployeeTask || selectedEmployeeTask.id !== taskID || !selectedEmployeeTask.session_id) return;
    const stream = taskEventStream(selectedEmployeeTask);
    if (!stream.source || !stream.healthy || stream.source.readyState === EventSource.CLOSED) {
      connectTaskEvents(selectedEmployeeTask, true);
    }
  })().finally(() => { taskCheckpointRefresh = null; });
  return taskCheckpointRefresh;
}

async function refreshEmployeeTask() {
  await refreshTaskCheckpoint(true);
}

function openEmployeeTaskInAgent() {
  if (!selectedEmployeeTask || !selectedEmployeeTask.session_id) return;
  switchWorkbenchView('agent');
  openSession(selectedEmployeeTask.session_id).catch(error => toast(error.message, true));
}

$('#task-new').addEventListener('click', () => startTaskCreate().catch(error => showTaskError(error)));
$('#task-create-close').addEventListener('click', showTaskEmpty);
$('#task-create-panel').addEventListener('submit', createEmployeeTask);
$('#task-create-employee').addEventListener('change', () => loadTaskCreateContext($('#task-create-employee').value).catch(error => showTaskError(error)));
$('#task-employee-filter').addEventListener('change', renderEmployeeTaskList);
$('#task-project-filter').addEventListener('change', renderEmployeeTaskList);
$('#task-state-filter').addEventListener('change', renderEmployeeTaskList);
$('#task-updated-filter').addEventListener('change', renderEmployeeTaskList);
$('#task-start').addEventListener('click', () => mutateEmployeeTask('start'));
$('#task-resume').addEventListener('click', () => mutateEmployeeTask('resume'));
$('#task-cancel').addEventListener('click', () => mutateEmployeeTask('cancel'));
$('#task-refresh').addEventListener('click', refreshEmployeeTask);
$('#task-open-agent').addEventListener('click', openEmployeeTaskInAgent);

(function bootEmployeeTasks() {
  if (localStorage.getItem('gohermit.view') === 'employee-tasks') {
    switchWorkbenchView('employee-tasks');
  }
})();
