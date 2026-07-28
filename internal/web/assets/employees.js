let employeeSummaries = [];
let selectedEmployeeSummary = null;
let selectedEmployeeRecord = null;
let employeeCatalogSkills = [];
let employeeProjects = [];
let employeeWizardStep = 0;
let selectedEmployeeTab = 'overview';
let employeeWizardRecord = null;
let employeeWizardReadiness = null;
let employeeWizardKnowledgeSaved = false;

function employeeSkillKey(skill) {
  return `${skill.skill_id}\0${skill.version}\0${skill.digest}`;
}

function employeeSkillByKey(key) {
  return employeeCatalogSkills.find(item => employeeSkillKey(item) === key);
}

function parseSkillConfiguration(raw, skill) {
  let value;
  try {
    value = JSON.parse(String(raw || '{}'));
  } catch (_) {
    throw new Error(`${skill.skill_id}@${skill.version} configuration must be valid JSON.`);
  }
  if (!value || Array.isArray(value) || typeof value !== 'object') {
    throw new Error(`${skill.skill_id}@${skill.version} configuration must be a JSON object.`);
  }
  if (skill.kind === 'skill_md_adapter' && Object.keys(value).length) {
    throw new Error('SKILL.md Adapter configuration must remain empty and cannot expand permissions.');
  }
  const schema = skill.configuration_schema || {};
  for (const name of schema.required || []) {
    if (!(name in value)) throw new Error(`${skill.skill_id}@${skill.version} configuration requires ${name}.`);
  }
  if (schema.additionalProperties === false && schema.properties) {
    for (const name of Object.keys(value)) {
      if (!(name in schema.properties)) throw new Error(`${skill.skill_id}@${skill.version} configuration contains unknown field ${name}.`);
    }
  }
  for (const [name, rule] of Object.entries(schema.properties || {})) {
    if (!(name in value) || !rule || !rule.type) continue;
    const actual = Array.isArray(value[name]) ? 'array' : value[name] === null ? 'null' : typeof value[name];
    if (actual !== rule.type) throw new Error(`${skill.skill_id}@${skill.version} configuration field ${name} must be ${rule.type}.`);
  }
  return value;
}

function showEmployeeError(error, target = '#employee-detail-error') {
  const panel = $(target);
  if (panel) {
    panel.textContent = error && error.message ? error.message : String(error);
    panel.classList.remove('hidden');
  }
  toast(error && error.message ? error.message : String(error), true);
}

function clearEmployeeError(target = '#employee-detail-error') {
  const panel = $(target);
  if (!panel) return;
  panel.textContent = '';
  panel.classList.add('hidden');
}

async function loadEmployees(openSaved = true) {
  const state = $('#employee-state-filter').value;
  const [page, projects, skills] = await Promise.all([
    request(`/api/employees?limit=100${state ? `&state=${encodeURIComponent(state)}` : ''}`),
    request('/api/projects'),
    request('/api/skills'),
  ]);
  employeeSummaries = page.employees || [];
  employeeProjects = projects.projects || [];
  employeeCatalogSkills = skills.skills || [];
  renderEmployeeList();
  if (!openSaved) return;
  const savedID = localStorage.getItem('gohermit.employee');
  const saved = employeeSummaries.find(item => item.id === savedID);
  if (saved) await openEmployee(saved.id, selectedEmployeeTab);
}

function renderEmployeeList() {
  const root = $('#employee-list');
  root.replaceChildren();
  if (!employeeSummaries.length) {
    const empty = document.createElement('div');
    empty.className = 'sidebar-empty';
    empty.textContent = 'No Employees yet';
    root.append(empty);
    return;
  }
  for (const summary of employeeSummaries) {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = `resource-list-item ${selectedEmployeeSummary && selectedEmployeeSummary.id === summary.id ? 'active' : ''}`;
    button.dataset.testid = 'employee-card';
    button.innerHTML = '<i class="resource-list-avatar"></i><div><strong></strong><span></span><small></small></div>';
    button.querySelector('i').textContent = employeeInitials(summary.name);
    button.querySelector('strong').textContent = summary.name;
    button.querySelector('span').textContent = summary.job_title;
    button.querySelector('small').textContent = `${summary.state} · r${summary.revision}`;
    button.addEventListener('click', () => openEmployee(summary.id));
    root.append(button);
  }
}

function employeeInitials(name) {
  const value = String(name || '').trim();
  if (!value) return 'EE';
  const parts = value.split(/\s+/).filter(Boolean);
  return (parts.length > 1 ? `${parts[0][0]}${parts[parts.length - 1][0]}` : value.slice(0, 2)).toUpperCase();
}

function showEmployeeEmpty() {
  $('#employee-empty').classList.remove('hidden');
  $('#employee-wizard').classList.add('hidden');
  $('#employee-detail').classList.add('hidden');
}

function startEmployeeWizard() {
  selectedEmployeeSummary = null;
  selectedEmployeeRecord = null;
  employeeWizardStep = 0;
  employeeWizardRecord = null;
  employeeWizardReadiness = null;
  employeeWizardKnowledgeSaved = false;
  $('#employee-wizard-form').reset();
  $('#employee-charter').value = 'Deliver bounded, verifiable work for the Owner.';
  $('#employee-memory-candidates').checked = true;
  $('#employee-memory-facts').value = '16';
  $('#employee-memory-bytes').value = '32768';
  $('#employee-capabilities').value = 'read';
  $('#employee-max-calls').value = '8';
  $('#employee-max-tokens').value = '100000';
  $('#employee-timeout').value = '3600';
  clearEmployeeError('#employee-wizard-error');
  fillEmployeeProviderChoices();
  renderWizardSkills();
  renderWizardProject();
  renderEmployeeWizardStep();
  $('#employee-empty').classList.add('hidden');
  $('#employee-detail').classList.add('hidden');
  $('#employee-wizard').classList.remove('hidden');
  renderEmployeeList();
  $('#employee-id').focus();
}

function fillEmployeeProviderChoices(preferred = {}) {
  const companies = catalog && Array.isArray(catalog.available_companies) ? catalog.available_companies : [];
  setOptions($('#employee-company'), companies, preferred.company || '', item => item.label);
  fillEmployeeAccessChoices(preferred);
  setOptions($('#employee-agent'), catalog && catalog.agents ? catalog.agents : [], preferred.agent || '', item => item.label);
  $('#employee-provider-note').textContent = companies.length
    ? 'Unavailable or expired Provider/Access/Model entries are intentionally hidden.'
    : 'No ready Provider/Access/Model is available. Configure access in Settings first.';
}

function fillEmployeeAccessChoices(preferred = {}) {
  const companies = catalog && Array.isArray(catalog.available_companies) ? catalog.available_companies : [];
  const company = companies.find(item => item.id === $('#employee-company').value);
  setOptions($('#employee-access'), company ? company.access || [] : [], preferred.access || '', item => item.label);
  fillEmployeeModelChoices(preferred.model || '');
}

function fillEmployeeModelChoices(preferred = '') {
  const companies = catalog && Array.isArray(catalog.available_companies) ? catalog.available_companies : [];
  const company = companies.find(item => item.id === $('#employee-company').value);
  const access = company && (company.access || []).find(item => item.id === $('#employee-access').value);
  setOptions($('#employee-model'), access ? access.models || [] : [], preferred, item => item.label);
}

function renderWizardSkills() {
  const root = $('#employee-wizard-skills');
  root.replaceChildren();
  if (!employeeCatalogSkills.length) {
    root.innerHTML = '<div class="review-empty">No configured Skills. The Employee can be created without one.</div>';
    return;
  }
  for (const skill of employeeCatalogSkills) {
    const label = document.createElement('label');
    label.className = 'selection-item';
    label.dataset.testid = 'wizard-skill';
    label.innerHTML = '<input type="checkbox"><div><strong></strong><span></span><small></small><label class="skill-config">Configuration JSON<textarea data-testid="skill-configuration" rows="2">{}</textarea></label></div>';
    label.querySelector('input').dataset.skillKey = employeeSkillKey(skill);
    label.querySelector('strong').textContent = skill.title || skill.skill_id;
    const requested = skill.kind === 'skill_md_adapter' ? [] : (skill.requested_capabilities || []);
    const employeeCeiling = $('#employee-capabilities').value.split(',').map(value => value.trim()).filter(Boolean);
    const effective = requested.filter(capability => employeeCeiling.includes(capability));
    label.querySelector('span').textContent = `${skill.description || 'Instruction context'} · requested: ${requested.join(', ') || 'zero capabilities'} · effective intersection: ${effective.join(', ') || 'none'}`;
    label.querySelector('small').textContent = `${skill.kind} · ${skill.skill_id}@${skill.version} · ${skill.digest}`;
    const configuration = label.querySelector('textarea');
    configuration.disabled = skill.kind === 'skill_md_adapter';
    configuration.addEventListener('click', event => event.stopPropagation());
    root.append(label);
  }
}

function renderWizardProject() {
  const root = $('#employee-wizard-project');
  root.replaceChildren();
  if (!employeeProjects.length) {
    root.innerHTML = '<div class="form-errors">The current Service Workspace is unavailable.</div>';
    return;
  }
  for (const project of employeeProjects) {
    const label = document.createElement('label');
    label.className = 'selection-item';
    label.innerHTML = '<input type="radio" name="employee-project" checked><div><strong></strong><span></span><small></small></div>';
    label.querySelector('input').value = project.id;
    label.querySelector('strong').textContent = project.label;
    label.querySelector('span').textContent = project.workspace_real_path;
    label.querySelector('small').textContent = 'Current Service Workspace only';
    root.append(label);
  }
}

function renderEmployeeWizardStep() {
  document.querySelectorAll('[data-wizard-step]').forEach((section, index) => section.classList.toggle('hidden', index !== employeeWizardStep));
  $('#employee-wizard-progress').textContent = `STEP ${employeeWizardStep + 1} / 9`;
  $('#wizard-back').disabled = employeeWizardStep === 0 || Boolean(employeeWizardRecord);
  $('#wizard-next').classList.toggle('hidden', employeeWizardStep === 8);
  $('#employee-create').classList.toggle('hidden', employeeWizardStep !== 8);
  if (employeeWizardStep === 8) {
    renderWizardReadiness();
  }
}

function validateEmployeeWizardStep() {
  clearEmployeeError('#employee-wizard-error');
  if (employeeWizardStep === 0) {
    for (const input of [$('#employee-id'), $('#employee-name'), $('#employee-job-title')]) {
      if (!input.reportValidity()) return false;
    }
    if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test($('#employee-id').value)) throw new Error('Employee ID must be path-safe and at most 128 bytes.');
    if ($('#employee-avatar-kind').value === 'emoji') {
      const avatar = $('#employee-avatar-value').value.trim();
      if (!avatar || avatar.length > 8 || /[/\\:%\r\n]/.test(avatar)) throw new Error('Avatar must be a single bounded Emoji, not a path or URL.');
    }
  }
  if (employeeWizardStep === 1 && (!$('#employee-company').value || !$('#employee-access').value || !$('#employee-model').value || !$('#employee-agent').value)) {
    throw new Error('Choose a ready Provider, Access, Model, and Agent.');
  }
  if (employeeWizardStep === 3) selectedWizardSkills();
  if (employeeWizardStep === 4) {
    const kind = $('#employee-knowledge-kind').value;
    if (kind && (!$('#employee-knowledge-id').value.trim() || !$('#employee-knowledge-title').value.trim())) throw new Error('Knowledge Source ID and Title are required.');
    if (kind === 'manual_text' && !$('#employee-knowledge-text').value.trim()) throw new Error('Manual Text cannot be empty.');
    if (kind && kind !== 'manual_text' && !/^[^%\\:\r\n]+$/.test($('#employee-knowledge-path').value.trim())) throw new Error('Knowledge path must be a canonical relative path.');
  }
  if (employeeWizardStep === 6 && !$('#employee-wizard-project input:checked')) throw new Error('Select the current Service Workspace.');
  if (employeeWizardStep === 7) {
    for (const input of [$('#employee-max-calls'), $('#employee-max-tokens'), $('#employee-timeout'), $('#employee-memory-facts'), $('#employee-memory-bytes')]) {
      if (!input.reportValidity()) return false;
    }
    if (!splitBoundedLines($('#employee-capabilities').value.replaceAll(',', '\n')).length) throw new Error('At least one bounded capability is required.');
  }
  return true;
}

async function employeeWizardNext() {
  try {
    if (!validateEmployeeWizardStep()) return;
    if (employeeWizardStep === 7) await prepareWizardReadiness();
    employeeWizardStep = Math.min(8, employeeWizardStep + 1);
    renderEmployeeWizardStep();
  } catch (error) {
    showEmployeeError(error, '#employee-wizard-error');
  }
}

function splitBoundedLines(value) {
  return String(value || '').split(/\r?\n/).map(item => item.trim()).filter(Boolean);
}

function selectedWizardSkills() {
  return [...$('#employee-wizard-skills').querySelectorAll('input:checked')].map(input => {
    const skill = employeeSkillByKey(input.dataset.skillKey);
    if (!skill) throw new Error('Selected Skill version is no longer in the configured Catalog. Reload and choose an explicit version.');
    const configuration = parseSkillConfiguration(input.closest('.selection-item').querySelector('textarea').value, skill);
    return {
      skill_id: skill.skill_id,
      version: skill.version,
      digest: skill.digest,
      configuration,
      enabled: true,
    };
  });
}

function wizardKnowledgeSource() {
  const kind = $('#employee-knowledge-kind').value;
  if (!kind) return null;
  return {
    id: $('#employee-knowledge-id').value.trim(),
    kind,
    title: $('#employee-knowledge-title').value.trim(),
    ...(kind === 'manual_text'
      ? {manual_text: $('#employee-knowledge-text').value}
      : {relative_path: $('#employee-knowledge-path').value.trim()}),
  };
}

async function prepareWizardReadiness() {
  if (employeeWizardReadiness) return;
  if (!employeeWizardRecord) {
    const payload = employeeCreatePayload();
    employeeWizardRecord = await request('/api/employees', {
      method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(payload),
    });
  }
  const record = employeeWizardRecord;
  const source = wizardKnowledgeSource();
  if (source && !employeeWizardKnowledgeSaved) {
    await request(`/api/employees/${encodeURIComponent(record.employee.id)}/knowledge`, {
      method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(source),
    });
    employeeWizardKnowledgeSaved = true;
  }
  const [dryRun, skills, knowledge] = await Promise.all([
    request(`/api/employees/${encodeURIComponent(record.employee.id)}/dry-run`, {method: 'POST'}),
    request(`/api/employees/${encodeURIComponent(record.employee.id)}/skills`),
    request(`/api/employees/${encodeURIComponent(record.employee.id)}/knowledge?limit=32`),
  ]);
  employeeWizardReadiness = {dryRun, skills, knowledge};
}

function renderWizardReadiness() {
  const root = $('#employee-wizard-review');
  root.replaceChildren();
  if (!employeeWizardReadiness) {
    root.textContent = 'Run the server readiness check before finishing.';
    return;
  }
  for (const check of employeeWizardReadiness.dryRun.checks || []) {
    const row = document.createElement('div');
    row.className = 'context-line';
    row.innerHTML = '<strong></strong><span></span><em class="status-pill"></em>';
    row.querySelector('strong').textContent = check.name;
    row.querySelector('span').textContent = check.detail;
    row.querySelector('em').textContent = check.ready ? 'ready' : 'blocked';
    if (!check.ready) {
      const action = document.createElement('button');
      action.type = 'button';
      action.className = 'small-button';
      action.textContent = check.name.includes('provider') || check.name.includes('access') ? 'Open Settings' : 'Open Employee settings';
      action.addEventListener('click', () => {
        if (check.name.includes('provider') || check.name.includes('access')) openSettings();
        else createEmployeeFromWizard({preventDefault() {}}).then(() => openEmployeeTab('settings'));
      });
      row.append(action);
    }
    root.append(row);
  }
  const supplemental = [
    ['Skill digest', (employeeWizardReadiness.skills.bindings || []).every(item => item.status === 'current') ? 'All pinned versions and digests are current.' : 'A pinned Skill is missing or has digest drift. Return to Skills.'],
    ['Knowledge', `${(employeeWizardReadiness.knowledge.sources || []).length} server-indexed source(s).`],
    ['Budget', `Server persisted ${employeeWizardRecord.employee.budget_policy.max_model_calls} calls / ${employeeWizardRecord.employee.budget_policy.max_tokens} tokens.`],
    ['Concurrency', `Server persisted max ${employeeWizardRecord.employee.concurrency_policy.max_running_tasks} running Task.`],
  ];
  for (const [name, detail] of supplemental) {
    const row = document.createElement('div');
    row.className = 'context-line';
    row.innerHTML = '<strong></strong><span></span>';
    row.querySelector('strong').textContent = name;
    row.querySelector('span').textContent = detail;
    root.append(row);
  }
}

function employeeCreatePayload() {
  const employeeID = $('#employee-id').value.trim();
  const project = employeeProjects.find(item => item.id === ($('#employee-wizard-project input:checked') || {}).value) || employeeProjects[0];
  const capabilities = $('#employee-capabilities').value.split(',').map(item => item.trim()).filter(Boolean);
  const avatarKind = $('#employee-avatar-kind').value;
  return {
    employee: {
      id: employeeID,
      name: $('#employee-name').value.trim(),
      avatar: { kind: avatarKind, value: avatarKind === 'emoji' ? $('#employee-avatar-value').value.trim() : '' },
      job_title: $('#employee-job-title').value.trim(),
      charter: $('#employee-charter').value.trim(),
      responsibilities: splitBoundedLines($('#employee-responsibilities').value),
      behavior_boundaries: splitBoundedLines($('#employee-boundaries').value),
      default_selection: {
        company: $('#employee-company').value,
        access: $('#employee-access').value,
        model: $('#employee-model').value,
      },
      agent_profile: $('#employee-agent').value,
      skill_bindings: selectedWizardSkills(),
      project_binding_ids: project ? [`project-${employeeID}`] : [],
      permission_policy: { allowed_capabilities: capabilities, network_allowed: $('#employee-network').checked },
      budget_policy: {
        max_model_calls: Number($('#employee-max-calls').value),
        max_tokens: Number($('#employee-max-tokens').value),
        timeout_seconds: Number($('#employee-timeout').value),
      },
      concurrency_policy: { max_running_tasks: 1 },
      memory_policy: {
        candidate_generation: $('#employee-memory-candidates').checked,
        promotion: $('#employee-memory-candidates').checked ? 'owner_confirmation' : 'disabled',
        max_context_facts: Number($('#employee-memory-facts').value),
        max_context_bytes: Number($('#employee-memory-bytes').value),
      },
    },
    project_bindings: project ? [{
      id: `project-${employeeID}`,
      label: project.label,
      workspace_real_path: project.workspace_real_path,
      read_allowed: true,
      mutation_allowed: capabilities.some(value => value.includes('write')),
      allowed_tool_capabilities: capabilities,
      network_allowed: $('#employee-network').checked,
    }] : [],
  };
}

async function createEmployeeFromWizard(event) {
  event.preventDefault();
  clearEmployeeError('#employee-wizard-error');
  try {
    const record = employeeWizardRecord;
    if (!record || !employeeWizardReadiness) throw new Error('Complete the real server Dry Run before finishing.');
    toast(employeeWizardReadiness.dryRun.ready ? 'Employee created and ready' : 'Employee created; readiness is blocked');
    await loadEmployees(false);
    await openEmployee(record.employee.id);
  } catch (error) {
    showEmployeeError(error, '#employee-wizard-error');
  }
}

async function openEmployee(id, tab = 'overview') {
  clearEmployeeError();
  const record = await request(`/api/employees/${encodeURIComponent(id)}`);
  selectedEmployeeRecord = record;
  selectedEmployeeSummary = employeeSummaries.find(item => item.id === id) || {
    id, name: record.employee.name, job_title: record.employee.job_title,
    state: record.employee.state, revision: record.employee.revision,
  };
  localStorage.setItem('gohermit.employee', id);
  $('#employee-empty').classList.add('hidden');
  $('#employee-wizard').classList.add('hidden');
  $('#employee-detail').classList.remove('hidden');
  renderEmployeeHeader();
  renderEmployeeList();
  await openEmployeeTab(tab);
}

function renderEmployeeHeader() {
  const value = selectedEmployeeRecord.employee;
  $('#employee-detail-avatar').textContent = value.avatar.kind === 'emoji' ? value.avatar.value : employeeInitials(value.name);
  $('#employee-detail-name').textContent = value.name;
  $('#employee-detail-meta').textContent = `${value.job_title} · ${value.id} · revision ${value.revision}`;
  $('#employee-status').textContent = value.state;
  $('#employee-status').className = `status-pill ${value.state}`;
  $('#employee-disable').classList.toggle('hidden', value.state !== 'active');
  $('#employee-enable').classList.toggle('hidden', value.state !== 'disabled');
  $('#employee-archive').classList.toggle('hidden', value.state === 'archived');
}

async function openEmployeeTab(tab) {
  selectedEmployeeTab = tab;
  document.querySelectorAll('[data-employee-tab]').forEach(button => button.setAttribute('aria-selected', String(button.dataset.employeeTab === tab)));
  clearEmployeeError();
  const employeeID = selectedEmployeeRecord.employee.id;
  try {
    if (tab === 'overview') return renderEmployeeOverview();
    if (tab === 'settings') return renderEmployeeSettings();
    if (tab === 'skills') return renderEmployeeSkills(await request(`/api/employees/${encodeURIComponent(employeeID)}/skills`));
    if (tab === 'knowledge') return renderEmployeeKnowledge(await request(`/api/employees/${encodeURIComponent(employeeID)}/knowledge?limit=32`));
    if (tab === 'memory') {
      const [memory, pending] = await Promise.all([
        request(`/api/employees/${encodeURIComponent(employeeID)}/memory`),
        request(`/api/employees/${encodeURIComponent(employeeID)}/memory-candidates`),
      ]);
      return renderEmployeeMemory(memory, pending);
    }
    if (tab === 'projects') return renderEmployeeProjects();
    if (tab === 'tasks') return renderEmployeeTasks(await request(`/api/employees/${encodeURIComponent(employeeID)}/tasks?limit=100`));
    if (tab === 'activity') return renderEmployeeActivity(await request(`/api/employees/${encodeURIComponent(employeeID)}/activity?limit=100`));
  } catch (error) {
    showEmployeeError(error);
  }
}

function resourceSection(title, subtitle = '') {
  const section = document.createElement('section');
  section.className = 'resource-section';
  section.innerHTML = '<header><div><strong></strong><span></span></div></header>';
  section.querySelector('strong').textContent = title;
  section.querySelector('span').textContent = subtitle;
  return section;
}

async function renderEmployeeOverview() {
  const value = selectedEmployeeRecord.employee;
  const root = $('#employee-detail-content');
  root.replaceChildren();
  const [readiness, tasks] = await Promise.all([
    request(`/api/employees/${encodeURIComponent(value.id)}/dry-run`, {method: 'POST'}).catch(error => ({ready: false, checks: [{name: 'readiness', ready: false, detail: error.message}]})),
    request(`/api/employees/${encodeURIComponent(value.id)}/tasks?limit=100`).catch(() => ({tasks: []})),
  ]);
  const active = (tasks.tasks || []).find(task => !employeeTaskTerminalStates.has(task.state));
  const recentVerified = (tasks.tasks || []).find(task => task.state === 'completed');
  const cards = document.createElement('div');
  cards.className = 'resource-grid';
  for (const [label, content] of [
    ['Charter', value.charter],
    ['Model', `${value.default_selection.company} / ${value.default_selection.model}`],
    ['Agent Profile', value.agent_profile],
    ['Concurrency', `${value.concurrency_policy.max_running_tasks} running Task`],
    ['Effective policy ceiling', `${(value.permission_policy.allowed_capabilities || []).join(', ') || 'none'}${value.permission_policy.network_allowed ? ' + network' : ''}`],
    ['Budget', `${value.budget_policy.max_model_calls} calls · ${value.budget_policy.max_tokens} tokens · ${value.budget_policy.timeout_seconds}s`],
    ['Active Task', active ? `${active.state}: ${active.prompt}` : 'None'],
    ['Recent verification', recentVerified ? `${recentVerified.state}: ${recentVerified.prompt}` : 'No verified Task yet'],
    ['Readiness', readiness.ready ? 'Ready' : `Blocked: ${(readiness.checks || []).filter(check => !check.ready).map(check => check.detail).join('; ')}`],
    ['Memory', value.memory_policy.promotion],
    ['Projects', String(selectedEmployeeRecord.project_bindings.length)],
  ]) {
    const card = document.createElement('article');
    card.className = 'summary-card';
    card.innerHTML = '<span></span><strong></strong>';
    card.querySelector('span').textContent = label;
    card.querySelector('strong').textContent = content;
    cards.append(card);
  }
  root.append(cards);
}

function renderEmployeeSettings() {
  const root = $('#employee-detail-content');
  const value = selectedEmployeeRecord.employee;
  const archived = value.state === 'archived';
  const section = resourceSection('Settings', `Expected revision ${value.revision}`);
  const form = document.createElement('form');
  form.className = 'form-grid';
  form.innerHTML = '<label>Name<input class="settings-name" maxlength="8192" required></label><label>Job title<input class="settings-title" maxlength="8192" required></label><label class="wide">Charter<textarea class="settings-charter" maxlength="8192" required></textarea></label><label>Capabilities<input class="settings-capabilities" maxlength="8192"></label><label>Max model calls<input class="settings-calls" type="number" min="1" max="1000"></label><label>Max tokens<input class="settings-tokens" type="number" min="1" max="10000000"></label><label>Timeout seconds<input class="settings-timeout" type="number" min="1" max="86400"></label><button class="small-button primary" type="submit">Save Settings</button>';
  form.querySelector('.settings-name').value = value.name;
  form.querySelector('.settings-title').value = value.job_title;
  form.querySelector('.settings-charter').value = value.charter;
  form.querySelector('.settings-capabilities').value = (value.permission_policy.allowed_capabilities || []).join(', ');
  form.querySelector('.settings-calls').value = value.budget_policy.max_model_calls;
  form.querySelector('.settings-tokens').value = value.budget_policy.max_tokens;
  form.querySelector('.settings-timeout').value = value.budget_policy.timeout_seconds;
  form.querySelectorAll('input, textarea, button').forEach(control => { control.disabled = archived; });
  form.addEventListener('submit', saveEmployeeSettings);
  section.append(form);
  if (archived) {
    const note = document.createElement('div');
    note.className = 'form-errors';
    note.textContent = 'Archived Employees remain readable, but configuration is immutable.';
    section.append(note);
  }
  root.replaceChildren(section);
}

async function saveEmployeeSettings(event) {
  event.preventDefault();
  const form = event.currentTarget;
  if (!form.reportValidity()) return;
  const proposed = structuredClone(selectedEmployeeRecord.employee);
  proposed.name = form.querySelector('.settings-name').value.trim();
  proposed.job_title = form.querySelector('.settings-title').value.trim();
  proposed.charter = form.querySelector('.settings-charter').value.trim();
  proposed.permission_policy.allowed_capabilities = form.querySelector('.settings-capabilities').value.split(',').map(value => value.trim()).filter(Boolean);
  proposed.budget_policy = {
    max_model_calls: Number(form.querySelector('.settings-calls').value),
    max_tokens: Number(form.querySelector('.settings-tokens').value),
    timeout_seconds: Number(form.querySelector('.settings-timeout').value),
  };
  try {
    selectedEmployeeRecord = await request(`/api/employees/${encodeURIComponent(proposed.id)}`, {
      method: 'PUT', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({expected_revision: selectedEmployeeRecord.employee.revision, employee: proposed, project_bindings: selectedEmployeeRecord.project_bindings}),
    });
    renderEmployeeHeader();
    renderEmployeeSettings();
  } catch (error) { showEmployeeError(error); }
}

function renderEmployeeSkills(result) {
  const root = $('#employee-detail-content');
  const section = resourceSection('Skills', 'Pinned digest and compatibility status');
  const statuses = new Map((result.bindings || []).map(item => [employeeSkillKey(item.binding), item]));
  const visibleByKey = new Map(employeeCatalogSkills.map(skill => [employeeSkillKey(skill), skill]));
  for (const item of result.bindings || []) {
    const key = employeeSkillKey(item.binding);
    if (!visibleByKey.has(key)) {
      visibleByKey.set(key, {
        skill_id: item.binding.skill_id, version: item.binding.version, digest: item.binding.digest,
        title: item.binding.skill_id, description: 'Pinned version is unavailable or its digest is stale.', kind: item.kind || 'missing',
        requested_capabilities: [], configuration_schema: {},
      });
    }
  }
  const visible = [...visibleByKey.values()];
  for (const skill of visible) {
    const status = statuses.get(employeeSkillKey(skill));
    const row = document.createElement('label');
    row.className = 'resource-row';
    row.dataset.testid = 'employee-skill';
    row.innerHTML = '<div><strong></strong><span></span><small></small><label class="skill-config">Configuration JSON<textarea data-testid="skill-configuration" rows="2">{}</textarea></label></div><div class="resource-row-actions"><em class="status-pill"></em><input type="checkbox"></div>';
    row.querySelector('strong').textContent = skill.title || skill.skill_id;
    const requested = skill.kind === 'skill_md_adapter' ? [] : (skill.requested_capabilities || []);
    const employeeCeiling = selectedEmployeeRecord.employee.permission_policy.allowed_capabilities || [];
    const effective = requested.filter(capability => employeeCeiling.includes(capability));
    row.querySelector('span').textContent = `${skill.description || `${skill.kind || status?.kind || 'Skill'} context`} · requested: ${requested.join(', ') || 'zero capabilities'} · effective intersection: ${effective.join(', ') || 'none'}`;
    row.querySelector('small').textContent = `${skill.kind || status?.kind || 'skill'} · ${skill.skill_id}@${skill.version} · ${skill.digest || ''}`;
    row.querySelector('em').textContent = status ? status.status : 'available';
    row.querySelector('input').checked = Boolean(status && status.binding.enabled);
    row.querySelector('input').dataset.skillKey = employeeSkillKey(skill);
    const configuration = row.querySelector('textarea');
    configuration.value = JSON.stringify((status && status.binding.configuration) || {}, null, 2);
    configuration.disabled = skill.kind === 'skill_md_adapter' || selectedEmployeeRecord.employee.state === 'archived' || !employeeCatalogSkills.some(item => employeeSkillKey(item) === employeeSkillKey(skill));
    row.querySelector('input').disabled = selectedEmployeeRecord.employee.state === 'archived' || !employeeCatalogSkills.some(item => employeeSkillKey(item) === employeeSkillKey(skill));
    section.append(row);
  }
  const header = section.querySelector('header');
  const save = document.createElement('button');
  save.className = 'small-button';
  save.textContent = 'Save bindings';
  save.disabled = selectedEmployeeRecord.employee.state === 'archived';
  save.addEventListener('click', () => saveEmployeeSkills(section, result.revision));
  header.append(save);
  root.replaceChildren(section);
}

async function saveEmployeeSkills(section, revision) {
  try {
    const bindings = [...section.querySelectorAll('input[type=checkbox]:checked')].map(input => {
      const skill = employeeSkillByKey(input.dataset.skillKey);
      if (!skill) throw new Error('Pinned Skill version/digest is unavailable; it cannot be silently replaced.');
      const configuration = parseSkillConfiguration(input.closest('.resource-row').querySelector('textarea').value, skill);
      return {skill_id: skill.skill_id, version: skill.version, digest: skill.digest, configuration, enabled: true};
    });
    const record = await request(`/api/employees/${encodeURIComponent(selectedEmployeeRecord.employee.id)}/skills`, {
      method: 'PUT', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({expected_revision: revision, bindings}),
    });
    selectedEmployeeRecord = record;
    renderEmployeeHeader();
    await openEmployeeTab('skills');
  } catch (error) { showEmployeeError(error); }
}

function renderEmployeeKnowledge(data) {
  const root = $('#employee-detail-content');
  const sources = resourceSection('Knowledge sources', 'Deterministic local index');
  const add = document.createElement('div');
  add.className = 'resource-row';
  add.innerHTML = '<div class="form-grid"><label>Kind<select class="knowledge-kind"><option value="manual_text">Manual Text</option><option value="file">Local file</option><option value="project_docs">Project docs</option></select></label><label>Source ID<input class="knowledge-id" maxlength="128"></label><label>Title<input class="knowledge-title" maxlength="8192"></label><label class="wide">Manual text or canonical relative path<textarea class="knowledge-text" maxlength="65536"></textarea></label></div><button class="small-button">Add</button>';
  add.querySelectorAll('input, textarea, select, button').forEach(control => { control.disabled = selectedEmployeeRecord.employee.state === 'archived'; });
  add.querySelector('button').addEventListener('click', () => addEmployeeKnowledge(add));
  sources.append(add);
  for (const source of data.sources || []) {
    const row = document.createElement('div');
    row.className = 'resource-row';
    row.dataset.testid = 'knowledge-source';
    row.innerHTML = '<div><strong></strong><span></span><small></small></div><div class="resource-row-actions"><button class="small-button refresh">Refresh</button><button class="small-button danger-text delete">Delete</button></div>';
    row.querySelector('strong').textContent = source.title;
    row.querySelector('span').textContent = `${source.kind} · ${source.status}`;
    row.querySelector('small').textContent = `${source.id} · digest ${source.digest}`;
    row.querySelectorAll('button').forEach(button => { button.disabled = selectedEmployeeRecord.employee.state === 'archived'; });
    row.querySelector('.refresh').addEventListener('click', () => refreshEmployeeKnowledge(source.id));
    row.querySelector('.delete').addEventListener('click', () => deleteEmployeeKnowledge(source.id));
    sources.append(row);
  }
  const citations = resourceSection('Citations', 'Stable bounded references');
  for (const result of data.results || []) {
    const row = document.createElement('div');
    row.className = 'resource-row';
    row.dataset.testid = 'knowledge-citation';
    row.innerHTML = '<div><strong></strong><span></span><small></small></div>';
    row.querySelector('strong').textContent = result.title;
    row.querySelector('span').textContent = result.citation.snippet;
    row.querySelector('small').textContent = `${result.citation.path}:${result.citation.start_line}-${result.citation.end_line}`;
    citations.append(row);
  }
  root.replaceChildren(sources, citations);
}

async function addEmployeeKnowledge(row) {
  const source = {
    id: row.querySelector('.knowledge-id').value.trim(),
    kind: row.querySelector('.knowledge-kind').value,
    title: row.querySelector('.knowledge-title').value.trim(),
    ...(row.querySelector('.knowledge-kind').value === 'manual_text'
      ? {manual_text: row.querySelector('.knowledge-text').value}
      : {relative_path: row.querySelector('.knowledge-text').value.trim()}),
  };
  try {
    await request(`/api/employees/${encodeURIComponent(selectedEmployeeRecord.employee.id)}/knowledge`, {
      method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(source),
    });
    await openEmployeeTab('knowledge');
  } catch (error) { showEmployeeError(error); }
}

async function refreshEmployeeKnowledge(sourceID) {
  try {
    await request(`/api/employees/${encodeURIComponent(selectedEmployeeRecord.employee.id)}/knowledge/${encodeURIComponent(sourceID)}/refresh`, {method: 'POST'});
    await openEmployeeTab('knowledge');
  } catch (error) { showEmployeeError(error); }
}

async function deleteEmployeeKnowledge(sourceID) {
  try {
    await request(`/api/employees/${encodeURIComponent(selectedEmployeeRecord.employee.id)}/knowledge/${encodeURIComponent(sourceID)}`, {method: 'DELETE'});
    await openEmployeeTab('knowledge');
  } catch (error) { showEmployeeError(error); }
}

function renderEmployeeMemory(memory, pending) {
  const root = $('#employee-detail-content');
  const candidates = resourceSection('Candidates', 'Owner confirmation required');
  for (const candidate of pending.candidates || []) {
    const row = document.createElement('div');
    row.className = 'resource-row';
    row.dataset.testid = 'memory-candidate';
    row.innerHTML = '<div><strong></strong><span></span><small></small></div><div class="resource-row-actions"><button class="small-button accept" data-testid="candidate-accept">Accept</button><button class="small-button danger-text reject">Reject</button></div>';
    row.querySelector('strong').textContent = candidate.category;
    row.querySelector('span').textContent = candidate.value;
    row.querySelector('small').textContent = memoryProvenance(candidate.provenance);
    row.querySelector('.accept').addEventListener('click', () => decideMemoryCandidate(candidate.id, true));
    row.querySelector('.reject').addEventListener('click', () => decideMemoryCandidate(candidate.id, false));
    row.querySelectorAll('button').forEach(button => { button.disabled = selectedEmployeeRecord.employee.state === 'archived'; });
    candidates.append(row);
  }
  const facts = resourceSection('Accepted memory', 'Employee-isolated long-term facts');
  for (const fact of memory.facts || []) {
    const row = document.createElement('div');
    row.className = 'resource-row';
    row.innerHTML = '<div><strong></strong><span></span><small></small></div><div class="resource-row-actions"><button class="small-button edit">Edit</button><button class="small-button danger-text forget" data-testid="memory-forget">Forget</button></div>';
    row.querySelector('strong').textContent = fact.category;
    row.querySelector('span').textContent = fact.value;
    row.querySelector('small').textContent = memoryProvenance(fact.provenance);
    row.querySelector('.edit').addEventListener('click', () => editMemoryFact(fact));
    row.querySelector('.forget').addEventListener('click', () => forgetMemoryFact(fact.id));
    row.querySelectorAll('button').forEach(button => { button.disabled = selectedEmployeeRecord.employee.state === 'archived'; });
    facts.append(row);
  }
  root.replaceChildren(candidates, facts);
}

function memoryProvenance(values = []) {
  return values.map(value => `${value.source_type}:${value.source_id}`).join(' · ');
}

async function decideMemoryCandidate(candidateID, accept) {
  try {
    await request(`/api/employees/${encodeURIComponent(selectedEmployeeRecord.employee.id)}/memory-candidates/${encodeURIComponent(candidateID)}${accept ? '/accept' : ''}`, {method: accept ? 'POST' : 'DELETE'});
    await openEmployeeTab('memory');
  } catch (error) { showEmployeeError(error); }
}

async function editMemoryFact(fact) {
  const value = window.prompt('Edit accepted Memory', fact.value);
  if (value === null || value === fact.value) return;
  try {
    await request(`/api/employees/${encodeURIComponent(selectedEmployeeRecord.employee.id)}/memory/${encodeURIComponent(fact.id)}`, {
      method: 'PUT', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({value}),
    });
    await openEmployeeTab('memory');
  } catch (error) { showEmployeeError(error); }
}

async function forgetMemoryFact(factID) {
  try {
    await request(`/api/employees/${encodeURIComponent(selectedEmployeeRecord.employee.id)}/memory/${encodeURIComponent(factID)}`, {method: 'DELETE'});
    await openEmployeeTab('memory');
  } catch (error) { showEmployeeError(error); }
}

function renderEmployeeProjects() {
  const root = $('#employee-detail-content');
  const section = resourceSection('Projects', 'Current Service Workspace only');
  for (const binding of selectedEmployeeRecord.project_bindings || []) {
    const row = document.createElement('div');
    row.className = 'resource-row';
    row.dataset.testid = 'employee-project';
    row.innerHTML = '<div><strong></strong><span></span><small></small></div><div class="resource-row-actions"><label><input class="project-mutation" type="checkbox"> Mutation</label><label><input class="project-network" type="checkbox"> Network</label></div>';
    row.querySelector('strong').textContent = binding.label;
    row.querySelector('span').textContent = binding.workspace_real_path;
    row.querySelector('small').textContent = `${binding.read_allowed ? 'read' : ''}${binding.mutation_allowed ? ' + mutation' : ''} · ${binding.workspace_fingerprint || ''}`;
    row.dataset.bindingID = binding.id;
    row.querySelector('.project-mutation').checked = binding.mutation_allowed;
    row.querySelector('.project-network').checked = binding.network_allowed;
    row.querySelectorAll('input').forEach(input => { input.disabled = selectedEmployeeRecord.employee.state === 'archived'; });
    section.append(row);
  }
  const save = document.createElement('button');
  save.className = 'small-button';
  save.textContent = 'Save Workspace policy';
  save.disabled = selectedEmployeeRecord.employee.state === 'archived';
  save.addEventListener('click', () => saveEmployeeProjects(section));
  section.querySelector('header').append(save);
  root.replaceChildren(section);
}

async function saveEmployeeProjects(section) {
  const proposed = structuredClone(selectedEmployeeRecord);
  for (const row of section.querySelectorAll('[data-binding-id]')) {
    const binding = proposed.project_bindings.find(item => item.id === row.dataset.bindingID);
    binding.mutation_allowed = row.querySelector('.project-mutation').checked;
    binding.network_allowed = row.querySelector('.project-network').checked;
  }
  try {
    selectedEmployeeRecord = await request(`/api/employees/${encodeURIComponent(proposed.employee.id)}`, {
      method: 'PUT', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({expected_revision: selectedEmployeeRecord.employee.revision, employee: proposed.employee, project_bindings: proposed.project_bindings}),
    });
    renderEmployeeHeader();
    renderEmployeeProjects();
  } catch (error) { showEmployeeError(error); }
}

function renderEmployeeTasks(page) {
  const root = $('#employee-detail-content');
  const section = resourceSection('Tasks', 'Execution state is projected from Session/Run');
  for (const task of page.tasks || []) {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'resource-row';
    button.dataset.testid = 'employee-task-row';
    button.innerHTML = '<div><strong></strong><span></span><small></small></div><em class="status-pill"></em>';
    button.querySelector('strong').textContent = task.prompt;
    button.querySelector('span').textContent = task.id;
    button.querySelector('small').textContent = task.session_id ? `Session ${task.session_id}` : 'Not started';
    button.querySelector('em').textContent = task.state;
    button.addEventListener('click', () => {
      switchWorkbenchView('employee-tasks');
      if (typeof openEmployeeTask === 'function') openEmployeeTask(task.id);
    });
    section.append(button);
  }
  root.replaceChildren(section);
}

function renderEmployeeActivity(page) {
  const root = $('#employee-detail-content');
  const section = resourceSection('Activity', 'Bounded lifecycle and references only');
  for (const event of page.events || []) {
    const row = document.createElement('div');
    row.className = 'resource-row';
    row.innerHTML = '<div><strong></strong><span></span><small></small></div>';
    row.querySelector('strong').textContent = event.type;
    row.querySelector('span').textContent = event.task_id || event.subject_id || `revision ${event.employee_revision || '—'}`;
    row.querySelector('small').textContent = new Date(event.time).toLocaleString();
    section.append(row);
  }
  root.replaceChildren(section);
}

async function transitionSelectedEmployee(action) {
  if ((action === 'disable' || action === 'archive') && !window.confirm(`${action === 'archive' ? 'Archive is terminal' : 'Disable is reversible'}. Continue?`)) return;
  try {
    const record = await request(`/api/employees/${encodeURIComponent(selectedEmployeeRecord.employee.id)}/${action}`, {
      method: 'POST', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({expected_revision: selectedEmployeeRecord.employee.revision}),
    });
    selectedEmployeeRecord = record;
    await loadEmployees(false);
    renderEmployeeHeader();
    renderEmployeeOverview();
  } catch (error) { showEmployeeError(error); }
}

async function dryRunSelectedEmployee() {
  try {
    const result = await request(`/api/employees/${encodeURIComponent(selectedEmployeeRecord.employee.id)}/dry-run`, {method: 'POST'});
    const root = $('#employee-detail-content');
    const section = resourceSection(result.ready ? 'Ready' : 'Not ready', 'Zero-side-effect readiness');
    for (const check of result.checks || []) {
      const row = document.createElement('div');
      row.className = 'resource-row';
      row.innerHTML = '<div><strong></strong><span></span></div><em class="status-pill"></em>';
      row.querySelector('strong').textContent = check.name;
      row.querySelector('span').textContent = check.detail;
      row.querySelector('em').textContent = check.ready ? 'ready' : 'blocked';
      section.append(row);
    }
    root.replaceChildren(section);
  } catch (error) { showEmployeeError(error); }
}

$('#employee-new').addEventListener('click', startEmployeeWizard);
$('#employee-wizard-close').addEventListener('click', showEmployeeEmpty);
$('#employee-list-refresh').addEventListener('click', () => loadEmployees(false).catch(error => showEmployeeError(error)));
$('#employee-state-filter').addEventListener('change', () => loadEmployees(false).catch(error => showEmployeeError(error)));
$('#employee-company').addEventListener('change', () => fillEmployeeAccessChoices());
$('#employee-access').addEventListener('change', () => fillEmployeeModelChoices());
$('#employee-avatar-kind').addEventListener('change', () => $('#employee-avatar-value-field').classList.toggle('hidden', $('#employee-avatar-kind').value !== 'emoji'));
$('#employee-knowledge-kind').addEventListener('change', () => {
  const manual = $('#employee-knowledge-kind').value === 'manual_text';
  $('#employee-knowledge-text-field').classList.toggle('hidden', !manual);
  $('#employee-knowledge-path-field').classList.toggle('hidden', manual || !$('#employee-knowledge-kind').value);
});
$('#wizard-next').addEventListener('click', () => employeeWizardNext());
$('#wizard-back').addEventListener('click', () => { employeeWizardStep = Math.max(0, employeeWizardStep - 1); renderEmployeeWizardStep(); });
$('#employee-wizard-form').addEventListener('submit', createEmployeeFromWizard);
document.querySelectorAll('[data-employee-tab]').forEach(button => button.addEventListener('click', () => openEmployeeTab(button.dataset.employeeTab)));
$('#employee-dry-run').addEventListener('click', dryRunSelectedEmployee);
$('#employee-disable').addEventListener('click', () => transitionSelectedEmployee('disable'));
$('#employee-enable').addEventListener('click', () => transitionSelectedEmployee('enable'));
$('#employee-archive').addEventListener('click', () => transitionSelectedEmployee('archive'));
document.addEventListener('gohermit:catalog', () => {
  if (!$('#employee-wizard').classList.contains('hidden')) fillEmployeeProviderChoices();
});

(function bootEmployees() {
  if (localStorage.getItem('gohermit.view') === 'employees') {
    switchWorkbenchView('employees');
  }
})();
