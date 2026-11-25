'use strict';
/* Auto-Deploy 前端（零依赖单页应用） */

const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => Array.from(el.querySelectorAll(s));
const main = $('#main');

const esc = v => String(v ?? '').replace(/[&<>"']/g, c => ({
  '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
}[c]));

const state = {
  hosts: [], tasks: [], runs: [], templates: [],
  tplCat: 'all', tplOS: 'all',
  runDetail: null, runLogs: {}, runLastSeq: {}, selHost: null,
  es: null, poll: null, runFinished: false,
};

const STATUS_TEXT = { pending: '等待', running: '执行中', success: '成功', failed: '失败', canceled: '已取消' };
const CAT_TEXT = { web: 'Web 服务', database: '数据库', runtime: '运行环境', system: '系统配置', network: '网络与安全', custom: '自定义' };
const badge = st => `<span class="badge b-${esc(st)}">${esc(STATUS_TEXT[st] || st)}</span>`;
const isTerminal = st => st === 'success' || st === 'failed' || st === 'canceled';
const fmtTime = s => s ? new Date(s).toLocaleString('zh-CN', { hour12: false }) : '—';
function fmtDur(a, b) {
  if (!a) return '—';
  const sec = Math.max(0, Math.round(((b ? new Date(b) : new Date()) - new Date(a)) / 1000));
  return sec < 60 ? `${sec} 秒` : `${Math.floor(sec / 60)} 分 ${sec % 60} 秒`;
}

/* ---------------- HTTP ---------------- */

async function api(path, opts = {}) {
  const res = await fetch(path, {
    method: opts.method || 'GET',
    headers: opts.body ? { 'Content-Type': 'application/json' } : {},
    body: opts.body ? JSON.stringify(opts.body) : undefined,
  });
  if (res.status === 401 && !path.startsWith('/api/login')) {
    showLogin();
    throw new Error('未登录');
  }
  let data = {};
  try { data = await res.json(); } catch { /* 非 JSON 响应 */ }
  if (!res.ok) throw new Error(data.error || `请求失败 (HTTP ${res.status})`);
  return data;
}

function toast(msg, type = 'ok') {
  const el = document.createElement('div');
  el.className = 'toast' + (type === 'err' ? ' err' : '');
  el.textContent = msg;
  $('#toast-root').appendChild(el);
  setTimeout(() => el.remove(), 4000);
}

/* ---------------- modal ---------------- */

function openModal(title, body, foot = '') {
  closeModal();
  $('#modal-root').innerHTML = `
    <div class="overlay" id="modal-overlay">
      <div class="modal">
        <div class="modal-head"><h3>${title}</h3><button class="icon-btn" data-action="modal-close">✕</button></div>
        <div class="modal-body">${body}</div>
        ${foot ? `<div class="modal-foot">${foot}</div>` : ''}
      </div>
    </div>`;
  $('#modal-overlay').addEventListener('mousedown', e => {
    if (e.target.id === 'modal-overlay') closeModal();
  });
}
function closeModal() { $('#modal-root').innerHTML = ''; }
const saveFoot = formId =>
  `<button class="btn" data-action="modal-close">取消</button>
   <button class="btn primary" data-action="form-save" data-form="${formId}">保存</button>`;

/* ---------------- router ---------------- */

function route() {
  if (state.es) { state.es.close(); state.es = null; }
  if (state.poll) { clearInterval(state.poll); state.poll = null; }
  const hash = location.hash || '#/hosts';
  $$('.sidebar nav a').forEach(a => a.classList.toggle('active', hash.startsWith(a.getAttribute('href'))));
  const runMatch = hash.match(/^#\/runs\/([0-9a-f]+)/);
  if (runMatch) return renderRunDetail(runMatch[1]);
  if (hash.startsWith('#/tasks')) return renderTasks(hash.split('/')[2] || '');
  if (hash.startsWith('#/templates')) return renderTemplates();
  if (hash.startsWith('#/runs')) return renderRuns();
  return renderHosts();
}
window.addEventListener('hashchange', route);

/* ---------------- hosts ---------------- */

async function renderHosts() {
  main.innerHTML = `
    <div class="page-head"><h2>主机管理</h2>
      <button class="btn primary" data-action="host-new">＋ 添加主机</button></div>
    <div class="card" id="host-table">加载中…</div>`;
  try { state.hosts = await api('/api/hosts'); } catch (e) { $('#host-table').textContent = e.message; return; }
  drawHostTable();
}

function drawHostTable() {
  const el = $('#host-table');
  if (!el) return;
  if (!state.hosts.length) {
    el.innerHTML = '<div class="empty">还没有主机，点击右上角「添加主机」开始。<br><br>Linux 目标需开启 SSH 与 bash；Windows 目标需安装 OpenSSH Server。</div>';
    return;
  }
  el.innerHTML = `<table>
    <thead><tr><th>名称</th><th>地址</th><th>用户</th><th>系统</th><th>认证</th><th>标签</th><th class="ops">操作</th></tr></thead>
    <tbody>${state.hosts.map(h => `<tr>
      <td>${esc(h.name)}</td>
      <td class="mono">${esc(h.addr)}:${h.port}</td>
      <td>${esc(h.user)}</td>
      <td>${h.os_type === 'windows' ? '🪟 Windows' : '🐧 Linux'}</td>
      <td>${h.auth_type === 'key' ? '私钥' : '密码'}</td>
      <td>${esc(h.tags) || '—'}</td>
      <td class="ops">
        <button class="btn sm" data-action="host-test" data-id="${h.id}">测试</button>
        <button class="btn sm" data-action="host-edit" data-id="${h.id}">编辑</button>
        <button class="btn sm danger" data-action="host-del" data-id="${h.id}">删除</button>
      </td></tr>`).join('')}</tbody></table>`;
}

function secretField(isKey, editing) {
  if (isKey) {
    return `私钥（PEM 格式）<textarea name="secret" rows="6" class="mono" placeholder="${editing ? '留空则保持原私钥不变' : '-----BEGIN OPENSSH PRIVATE KEY-----'}"></textarea>`;
  }
  return `密码<input name="secret" type="password" placeholder="${editing ? '留空则保持原密码不变' : 'SSH 登录密码'}">`;
}

function hostForm(h = {}) {
  const isKey = (h.auth_type || 'password') === 'key';
  const editing = !!h.id;
  return `<form id="host-form" class="form-grid">
    <input type="hidden" name="id" value="${esc(h.id || '')}">
    <label>名称<input name="name" required value="${esc(h.name || '')}" placeholder="如 web-01"></label>
    <label>地址<input name="addr" required value="${esc(h.addr || '')}" placeholder="IP 或域名"></label>
    <label>端口<input name="port" type="number" min="1" max="65535" value="${h.port || 22}"></label>
    <label>SSH 用户<input name="user" required value="${esc(h.user || '')}" placeholder="root / Administrator"></label>
    <label>系统类型<select name="os_type">
      <option value="linux" ${h.os_type !== 'windows' ? 'selected' : ''}>Linux（bash）</option>
      <option value="windows" ${h.os_type === 'windows' ? 'selected' : ''}>Windows（PowerShell）</option>
    </select></label>
    <label>认证方式<select name="auth_type" id="host-auth">
      <option value="password" ${!isKey ? 'selected' : ''}>密码</option>
      <option value="key" ${isKey ? 'selected' : ''}>私钥</option>
    </select></label>
    <label class="span2" id="secret-field">${secretField(isKey, editing)}</label>
    <label class="span2" id="passphrase-field" ${isKey ? '' : 'hidden'}>私钥口令（可选）<input name="passphrase" type="password" placeholder="留空则保持原值不变"></label>
    <label class="span2">标签（可选）<input name="tags" value="${esc(h.tags || '')}" placeholder="如 prod,web"></label>
  </form>`;
}

async function submitHostForm(form) {
  const fd = new FormData(form);
  const body = Object.fromEntries(fd.entries());
  body.port = parseInt(body.port || '22', 10);
  const id = body.id;
  delete body.id;
  if (!id && !body.secret) return toast('请填写密码或私钥', 'err');
  try {
    if (id) await api('/api/hosts/' + id, { method: 'PUT', body });
    else await api('/api/hosts', { method: 'POST', body });
    closeModal();
    toast(id ? '主机已更新' : '主机已添加');
    state.hosts = await api('/api/hosts');
    drawHostTable();
  } catch (e) { toast(e.message, 'err'); }
}

async function testHost(id, btn) {
  btn.disabled = true;
  btn.textContent = '测试中…';
  try {
    const res = await api(`/api/hosts/${id}/test`, { method: 'POST' });
    const title = res.ok ? '✅ 连接成功' : '❌ 连接失败';
    const detail = res.ok ? '' : `<p class="error">${esc(res.error || '退出码 ' + res.exit_code)}</p>`;
    openModal('连接测试', `<p>${title}</p>${detail}<pre class="log-view" style="height:220px">${esc(res.output || '（无输出）')}</pre>`,
      '<button class="btn" data-action="modal-close">关闭</button>');
  } catch (e) { toast(e.message, 'err'); }
  btn.disabled = false;
  btn.textContent = '测试';
}

/* ---------------- tasks ---------------- */

async function renderTasks(id) {
  main.innerHTML = `
    <div class="page-head"><h2>部署任务</h2>
      <button class="btn primary" data-action="task-new">＋ 新建任务</button></div>
    <div class="tasks-layout">
      <div class="card task-list" id="task-list">加载中…</div>
      <div class="card task-editor" id="task-editor"></div>
    </div>`;
  try { state.tasks = await api('/api/tasks'); } catch (e) { $('#task-list').textContent = e.message; return; }
  drawTaskList(id);
  if (!id) {
    $('#task-editor').innerHTML = '<div class="empty">选择左侧任务进行编辑，或点击「新建任务」。</div>';
    return;
  }
  let task = null;
  if (id !== 'new') {
    task = state.tasks.find(t => t.id === id);
    if (!task) { try { task = await api('/api/tasks/' + id); } catch { /* 已删除 */ } }
    if (!task) {
      $('#task-editor').innerHTML = '<div class="empty">任务不存在或已被删除。</div>';
      return;
    }
  }
  drawTaskEditor(task);
}

function drawTaskList(activeId) {
  const el = $('#task-list');
  if (!el) return;
  if (!state.tasks.length) {
    el.innerHTML = '<div class="empty">暂无任务</div>';
    return;
  }
  el.innerHTML = state.tasks.map(t => `
    <div class="task-item ${t.id === activeId ? 'active' : ''}" data-action="task-open" data-id="${t.id}">
      <div class="task-item-name">${esc(t.name)}</div>
      <div class="muted sm">${t.os_type === 'windows' ? '🪟 Windows' : '🐧 Linux'} · ${t.steps.length} 个步骤</div>
    </div>`).join('');
}

function stepHTML(s, i) {
  return `<div class="step">
    <div class="step-head">
      <span class="step-no">#${i + 1}</span>
      <input name="step-name" value="${esc(s.name)}" placeholder="步骤名称（可选）">
      <button type="button" class="icon-btn" data-action="step-up" title="上移">↑</button>
      <button type="button" class="icon-btn" data-action="step-down" title="下移">↓</button>
      <button type="button" class="icon-btn" data-action="step-save-tpl" title="存为模板">📚</button>
      <button type="button" class="icon-btn danger" data-action="step-del" title="删除步骤">✕</button>
    </div>
    <textarea name="step-script" class="mono" rows="7" placeholder="bash 或 PowerShell 脚本">${esc(s.script)}</textarea>
  </div>`;
}

function drawTaskEditor(task) {
  const el = $('#task-editor');
  if (!el) return;
  const t = task || { name: '', description: '', os_type: 'linux', steps: [{ name: '', script: '' }] };
  el.innerHTML = `<form id="task-form">
    <input type="hidden" name="id" value="${esc(t.id || '')}">
    <div class="form-grid">
      <label>任务名称<input name="name" required value="${esc(t.name)}" placeholder="如 Nginx 初始化部署"></label>
      <label>目标系统<select name="os_type">
        <option value="linux" ${t.os_type !== 'windows' ? 'selected' : ''}>Linux（bash）</option>
        <option value="windows" ${t.os_type === 'windows' ? 'selected' : ''}>Windows（PowerShell）</option>
      </select></label>
      <label class="span2">描述<input name="description" value="${esc(t.description)}" placeholder="可选"></label>
    </div>
    <div class="steps-head">
      <h3>执行步骤（按顺序执行，任一步骤失败即中止该主机）</h3>
      <div class="head-actions">
        <button type="button" class="btn sm" data-action="tpl-pick">📚 从模板添加</button>
        <button type="button" class="btn sm" data-action="step-add">＋ 添加步骤</button>
      </div>
    </div>
    <div id="steps">${t.steps.map((s, i) => stepHTML(s, i)).join('')}</div>
    <div class="editor-foot">
      <button type="submit" class="btn primary">保存任务</button>
      ${t.id ? `<button type="button" class="btn success" data-action="task-run" data-id="${t.id}">▶ 执行</button>
      <button type="button" class="btn danger" data-action="task-del" data-id="${t.id}">删除任务</button>` : ''}
    </div>
  </form>`;
}

const collectSteps = () => $$('#steps .step').map(el => ({
  name: $('input[name="step-name"]', el).value,
  script: $('textarea', el).value,
}));
const redrawSteps = steps => { $('#steps').innerHTML = steps.map((s, i) => stepHTML(s, i)).join(''); };

async function submitTaskForm(form) {
  const fd = new FormData(form);
  const id = fd.get('id');
  const body = {
    name: fd.get('name'),
    description: fd.get('description'),
    os_type: fd.get('os_type'),
    steps: collectSteps(),
  };
  try {
    const saved = id
      ? await api('/api/tasks/' + id, { method: 'PUT', body })
      : await api('/api/tasks', { method: 'POST', body });
    toast('任务已保存');
    const target = '#/tasks/' + saved.id;
    if (location.hash === target) renderTasks(saved.id);
    else location.hash = target;
  } catch (e) { toast(e.message, 'err'); }
}

async function openRunPicker(taskId) {
  let task = state.tasks.find(t => t.id === taskId);
  try {
    if (!task) task = await api('/api/tasks/' + taskId);
    if (!state.hosts.length) state.hosts = await api('/api/hosts');
  } catch (e) { return toast(e.message, 'err'); }
  const targets = state.hosts.filter(h => h.os_type === task.os_type);
  if (!targets.length) return toast(`没有可用的 ${task.os_type} 主机，请先在「主机管理」中添加`, 'err');
  openModal(`执行任务：${esc(task.name)}`, `
    <p class="muted">选择目标主机（仅显示 ${task.os_type} 主机，多主机并行执行）：</p>
    <div class="host-picker">${targets.map(h => `
      <label class="pick"><input type="checkbox" value="${h.id}" checked>
        <span>${esc(h.name)}</span><span class="mono muted">${esc(h.addr)}</span></label>`).join('')}
    </div>`,
    `<button class="btn" data-action="modal-close">取消</button>
     <button class="btn primary" data-action="run-confirm" data-id="${task.id}">开始执行</button>`);
}

async function confirmRun(taskId) {
  const ids = $$('.host-picker input:checked').map(i => i.value);
  if (!ids.length) return toast('请至少选择一台主机', 'err');
  try {
    const run = await api(`/api/tasks/${taskId}/run`, { method: 'POST', body: { host_ids: ids } });
    closeModal();
    location.hash = '#/runs/' + run.id;
  } catch (e) { toast(e.message, 'err'); }
}

/* ---------------- templates ---------------- */

async function renderTemplates() {
  main.innerHTML = `
    <div class="page-head"><h2>模板库</h2>
      <button class="btn primary" data-action="tpl-new">＋ 新建模板</button></div>
    <p class="muted sm tpl-tip">可复用的部署脚本模板：在任务编辑器中「从模板添加」插入步骤，或把写好的步骤「存为模板」沉淀复用。</p>
    <div class="tpl-filters">
      <div class="chips" id="tpl-cats"></div>
      <div class="chips" id="tpl-os"></div>
    </div>
    <div id="tpl-grid">加载中…</div>`;
  try { state.templates = await api('/api/templates'); } catch (e) { $('#tpl-grid').textContent = e.message; return; }
  drawTemplateFilters();
  drawTemplateGrid();
}

function drawTemplateFilters() {
  const cats = $('#tpl-cats'), oses = $('#tpl-os');
  if (!cats || !oses) return;
  const chip = (label, active, action, val) =>
    `<button class="chip ${active ? 'active' : ''}" data-action="${action}" data-val="${val}">${label}</button>`;
  cats.innerHTML = chip('全部分类', state.tplCat === 'all', 'tpl-cat', 'all') +
    Object.entries(CAT_TEXT).map(([k, v]) => chip(v, state.tplCat === k, 'tpl-cat', k)).join('');
  oses.innerHTML = chip('全部系统', state.tplOS === 'all', 'tpl-os', 'all') +
    chip('🐧 Linux', state.tplOS === 'linux', 'tpl-os', 'linux') +
    chip('🪟 Windows', state.tplOS === 'windows', 'tpl-os', 'windows');
}

function drawTemplateGrid() {
  const el = $('#tpl-grid');
  if (!el) return;
  const list = state.templates.filter(t =>
    (state.tplCat === 'all' || t.category === state.tplCat) &&
    (state.tplOS === 'all' || t.os_type === state.tplOS));
  if (!list.length) {
    el.innerHTML = '<div class="card empty">没有匹配的模板。</div>';
    return;
  }
  el.innerHTML = `<div class="tpl-grid">${list.map(t => `
    <div class="card tpl-card">
      <div class="tpl-head">
        <span class="tpl-name">${esc(t.name)}</span>
        <span class="badge ${t.os_type === 'windows' ? 'b-os-win' : 'b-os-linux'}">${t.os_type === 'windows' ? '🪟 Windows' : '🐧 Linux'}</span>
      </div>
      <div class="muted sm tpl-meta">${esc(CAT_TEXT[t.category] || t.category)} · ${t.built_in ? '内置模板' : '自定义模板'}</div>
      <p class="tpl-desc">${esc(t.description) || '—'}</p>
      <div class="tpl-ops">
        <button class="btn sm" data-action="tpl-view" data-id="${t.id}">查看</button>
        <button class="btn sm" data-action="tpl-to-task" data-id="${t.id}">生成任务</button>
        ${t.built_in
          ? `<button class="btn sm" data-action="tpl-copy" data-id="${t.id}">复制为自定义</button>`
          : `<button class="btn sm" data-action="tpl-edit" data-id="${t.id}">编辑</button>
             <button class="btn sm danger" data-action="tpl-del" data-id="${t.id}">删除</button>`}
      </div>
    </div>`).join('')}</div>`;
}

function templateForm(t = {}) {
  return `<form id="tpl-form" class="form-grid">
    <input type="hidden" name="id" value="${esc(t.id || '')}">
    <label>模板名称<input name="name" required value="${esc(t.name || '')}" placeholder="如 Nginx 安装与启动"></label>
    <label>目标系统<select name="os_type">
      <option value="linux" ${t.os_type !== 'windows' ? 'selected' : ''}>Linux（bash）</option>
      <option value="windows" ${t.os_type === 'windows' ? 'selected' : ''}>Windows（PowerShell）</option>
    </select></label>
    <label>分类<select name="category">
      ${Object.entries(CAT_TEXT).map(([k, v]) => `<option value="${k}" ${(t.category || 'custom') === k ? 'selected' : ''}>${v}</option>`).join('')}
    </select></label>
    <label>描述<input name="description" value="${esc(t.description || '')}" placeholder="可选，说明用途与适用环境"></label>
    <label class="span2">脚本内容<textarea name="script" rows="12" class="mono" required placeholder="bash 或 PowerShell 脚本">${esc(t.script || '')}</textarea></label>
  </form>`;
}

async function submitTemplateForm(form) {
  const fd = new FormData(form);
  const body = Object.fromEntries(fd.entries());
  const id = body.id;
  delete body.id;
  try {
    if (id) await api('/api/templates/' + id, { method: 'PUT', body });
    else await api('/api/templates', { method: 'POST', body });
    closeModal();
    toast(id ? '模板已更新' : '模板已保存');
    state.templates = await api('/api/templates');
    drawTemplateFilters();
    drawTemplateGrid();
  } catch (e) { toast(e.message, 'err'); }
}

function viewTemplate(t) {
  openModal(`模板：${esc(t.name)}`, `
    <p class="muted sm">${esc(CAT_TEXT[t.category] || t.category)} · ${t.os_type === 'windows' ? '🪟 Windows（PowerShell）' : '🐧 Linux（bash）'} · ${t.built_in ? '内置模板' : '自定义模板'}</p>
    ${t.description ? `<p>${esc(t.description)}</p>` : ''}
    <pre class="mono tpl-script">${esc(t.script)}</pre>`,
    `<button class="btn" data-action="tpl-clipboard" data-id="${t.id}">复制脚本</button>
     ${t.built_in
       ? `<button class="btn primary" data-action="tpl-copy" data-id="${t.id}">复制为自定义</button>`
       : `<button class="btn primary" data-action="tpl-edit" data-id="${t.id}">编辑</button>`}`);
}

async function copyText(text) {
  try { await navigator.clipboard.writeText(text); }
  catch {
    const ta = document.createElement('textarea');
    ta.value = text;
    document.body.appendChild(ta);
    ta.select();
    document.execCommand('copy');
    ta.remove();
  }
  toast('脚本已复制到剪贴板');
}

async function taskFromTemplate(id) {
  const t = state.templates.find(x => x.id === id);
  if (!t) return;
  try {
    const task = await api('/api/tasks', {
      method: 'POST',
      body: { name: t.name, description: t.description, os_type: t.os_type, steps: [{ name: t.name, script: t.script }] },
    });
    toast('已从模板创建任务');
    location.hash = '#/tasks/' + task.id;
  } catch (e) { toast(e.message, 'err'); }
}

async function openTemplatePicker() {
  const os = $('#task-form select[name="os_type"]').value;
  if (!state.templates.length) {
    try { state.templates = await api('/api/templates'); } catch (e) { return toast(e.message, 'err'); }
  }
  const list = state.templates.filter(t => t.os_type === os);
  if (!list.length) return toast(`没有适用于 ${os} 的模板`, 'err');
  openModal('从模板添加步骤', `
    <p class="muted">选择模板，将作为新步骤追加到任务末尾（仅显示 ${os === 'windows' ? '🪟 Windows' : '🐧 Linux'} 模板）：</p>
    <div class="tpl-picker">${list.map(t => `
      <div class="pick-row">
        <div class="pick-info">
          <div>${esc(t.name)}</div>
          <div class="muted sm">${esc(CAT_TEXT[t.category] || t.category)}${t.built_in ? ' · 内置' : ''}${t.description ? ' · ' + esc(t.description) : ''}</div>
        </div>
        <button class="btn sm primary" data-action="tpl-insert" data-id="${t.id}">插入</button>
      </div>`).join('')}
    </div>`);
}

/* ---------------- runs ---------------- */

async function renderRuns() {
  main.innerHTML = `<div class="page-head"><h2>执行记录</h2></div><div class="card" id="run-table">加载中…</div>`;
  await refreshRuns();
  state.poll = setInterval(refreshRuns, 5000);
}

async function refreshRuns() {
  const el = $('#run-table');
  if (!el) return;
  try { state.runs = await api('/api/runs?limit=100'); } catch (e) { el.textContent = e.message; return; }
  if (!state.runs.length) {
    el.innerHTML = '<div class="empty">暂无执行记录。到「部署任务」中发起第一次部署吧。</div>';
    return;
  }
  el.innerHTML = `<table>
    <thead><tr><th>任务</th><th>状态</th><th>主机</th><th>开始时间</th><th>耗时</th><th class="ops">操作</th></tr></thead>
    <tbody>${state.runs.map(r => {
      const ok = r.hosts.filter(h => h.status === 'success').length;
      return `<tr>
        <td>${esc(r.task_name)}</td>
        <td>${badge(r.status)}</td>
        <td>${ok}/${r.hosts.length} 成功</td>
        <td>${fmtTime(r.created_at)}</td>
        <td>${fmtDur(r.started_at, r.finished_at)}</td>
        <td class="ops">
          <button class="btn sm" data-action="run-open" data-id="${r.id}">查看</button>
          ${!isTerminal(r.status) ? `<button class="btn sm danger" data-action="run-cancel" data-id="${r.id}">取消</button>` : ''}
        </td></tr>`;
    }).join('')}</tbody></table>`;
}

async function renderRunDetail(id) {
  main.innerHTML = '<div id="run-detail">加载中…</div>';
  let run;
  try { run = await api('/api/runs/' + id); } catch (e) { $('#run-detail').textContent = e.message; return; }
  state.runDetail = run;
  state.runLogs = {};
  state.runLastSeq = {};
  state.runFinished = isTerminal(run.status);
  run.hosts.forEach(h => { state.runLogs[h.id] = []; });
  state.selHost = run.hosts[0] ? run.hosts[0].id : null;
  drawRunDetail();
  const es = new EventSource(`/api/runs/${id}/logs`);
  state.es = es;
  es.onmessage = m => {
    let ev;
    try { ev = JSON.parse(m.data); } catch { return; }
    handleRunEvent(ev);
  };
  es.onerror = () => {
    // 已结束的流会被服务端关闭，避免 EventSource 无限重连。
    if (state.runFinished && state.es) { state.es.close(); state.es = null; }
  };
}

function drawRunDetail() {
  const run = state.runDetail;
  $('#run-detail').innerHTML = `
    <div class="page-head">
      <h2><a href="#/runs" class="back">←</a> ${esc(run.task_name)} <span id="run-badge">${badge(run.status)}</span></h2>
      <div class="head-actions">
        <a class="btn" href="/api/runs/${run.id}/logs?format=text" download>下载日志</a>
        ${!isTerminal(run.status) ? `<button class="btn danger" data-action="run-cancel" data-id="${run.id}">取消执行</button>` : ''}
      </div>
    </div>
    <div class="muted run-meta">执行 ID <span class="mono">${run.id}</span> · 开始于 ${fmtTime(run.started_at)} · 耗时 <span id="run-dur">${fmtDur(run.started_at, run.finished_at)}</span></div>
    <div class="host-tabs">${run.hosts.map(h => `
      <button class="tab ${h.id === state.selHost ? 'active' : ''}" data-action="run-tab" data-id="${h.id}">
        <span class="dot d-${h.status}"></span>${esc(h.host_name)}</button>`).join('')}
    </div>
    <pre class="log-view" id="log-pane"></pre>`;
  renderLogPane();
}

function renderLogPane() {
  const pane = $('#log-pane');
  if (!pane) return;
  const lines = state.runLogs[state.selHost] || [];
  pane.innerHTML = lines.map(l => `<div class="ln ln-${esc(l.stream || 'stdout')}">${esc(l.text)}</div>`).join('');
  pane.scrollTop = pane.scrollHeight;
}

function appendLogLine(ev) {
  const pane = $('#log-pane');
  if (!pane) return;
  const nearBottom = pane.scrollHeight - pane.scrollTop - pane.clientHeight < 60;
  const div = document.createElement('div');
  div.className = 'ln ln-' + (ev.stream || 'stdout');
  div.textContent = ev.text;
  pane.appendChild(div);
  while (pane.childNodes.length > 5000) pane.removeChild(pane.firstChild);
  if (nearBottom) pane.scrollTop = pane.scrollHeight;
}

function handleRunEvent(ev) {
  const run = state.runDetail;
  if (!run || ev.run_id !== run.id) return;
  if (ev.type === 'log') {
    const buf = state.runLogs[ev.run_host_id];
    if (!buf) return;
    // 断线重连时服务端会重放历史日志，按序号去重。
    const last = state.runLastSeq[ev.run_host_id] || 0;
    if (ev.seq && ev.seq <= last) return;
    if (ev.seq) state.runLastSeq[ev.run_host_id] = ev.seq;
    buf.push(ev);
    if (buf.length > 5000) buf.splice(0, buf.length - 5000);
    if (ev.run_host_id === state.selHost) appendLogLine(ev);
    return;
  }
  if (ev.type !== 'status') return;
  if (ev.run_host_id) {
    const rh = run.hosts.find(h => h.id === ev.run_host_id);
    if (rh) {
      rh.status = ev.status;
      if (ev.exit_code != null) rh.exit_code = ev.exit_code;
    }
    const dot = $(`.host-tabs .tab[data-id="${ev.run_host_id}"] .dot`);
    if (dot) dot.className = 'dot d-' + ev.status;
    return;
  }
  run.status = ev.status;
  const b = $('#run-badge');
  if (b) b.innerHTML = badge(ev.status);
  if (isTerminal(ev.status)) {
    state.runFinished = true;
    run.finished_at = new Date().toISOString();
    const dur = $('#run-dur');
    if (dur) dur.textContent = fmtDur(run.started_at, run.finished_at);
    const cbtn = $('[data-action="run-cancel"]');
    if (cbtn) cbtn.remove();
    if (state.es) { state.es.close(); state.es = null; }
  }
}

function selectRunTab(id) {
  state.selHost = id;
  $$('.host-tabs .tab').forEach(t => t.classList.toggle('active', t.dataset.id === id));
  renderLogPane();
}

/* ---------------- login ---------------- */

function showLogin() { $('#login-overlay').classList.remove('hidden'); }

async function submitLogin(form) {
  const fd = new FormData(form);
  try {
    await api('/api/login', { method: 'POST', body: { username: fd.get('username'), password: fd.get('password') } });
    $('#login-overlay').classList.add('hidden');
    $('#login-error').textContent = '';
    boot();
  } catch (e) { $('#login-error').textContent = e.message; }
}

async function boot() {
  try {
    const me = await api('/api/me');
    $('#version').textContent = 'v' + me.version;
    $('#login-overlay').classList.add('hidden');
    route();
  } catch { /* 未登录时已弹出登录框 */ }
}

/* ---------------- global events ---------------- */

document.addEventListener('click', async e => {
  const btn = e.target.closest('[data-action]');
  if (!btn) return;
  const { action, id } = btn.dataset;
  try {
    switch (action) {
      case 'modal-close': closeModal(); break;
      case 'form-save': $('#' + btn.dataset.form).requestSubmit(); break;
      case 'host-new': openModal('添加主机', hostForm(), saveFoot('host-form')); break;
      case 'host-edit': {
        const h = state.hosts.find(x => x.id === id);
        if (h) openModal('编辑主机', hostForm(h), saveFoot('host-form'));
        break;
      }
      case 'host-del':
        if (confirm('确定删除该主机？')) {
          await api('/api/hosts/' + id, { method: 'DELETE' });
          toast('主机已删除');
          state.hosts = await api('/api/hosts');
          drawHostTable();
        }
        break;
      case 'host-test': await testHost(id, btn); break;
      case 'task-new': location.hash = '#/tasks/new'; break;
      case 'task-open': {
        const target = '#/tasks/' + id;
        if (location.hash === target) renderTasks(id);
        else location.hash = target;
        break;
      }
      case 'task-run': await openRunPicker(id); break;
      case 'task-del':
        if (confirm('确定删除该任务？')) {
          await api('/api/tasks/' + id, { method: 'DELETE' });
          toast('任务已删除');
          location.hash = '#/tasks';
        }
        break;
      case 'run-confirm': await confirmRun(id); break;
      case 'run-open': location.hash = '#/runs/' + id; break;
      case 'run-cancel':
        await api('/api/runs/' + id + '/cancel', { method: 'POST' });
        toast('已发送取消指令');
        break;
      case 'run-tab': selectRunTab(id); break;
      case 'step-add': redrawSteps([...collectSteps(), { name: '', script: '' }]); break;
      case 'step-del': {
        const steps = collectSteps();
        const idx = $$('#steps .step').indexOf(btn.closest('.step'));
        steps.splice(idx, 1);
        redrawSteps(steps.length ? steps : [{ name: '', script: '' }]);
        break;
      }
      case 'step-up':
      case 'step-down': {
        const steps = collectSteps();
        const idx = $$('#steps .step').indexOf(btn.closest('.step'));
        const to = action === 'step-up' ? idx - 1 : idx + 1;
        if (to >= 0 && to < steps.length) {
          [steps[idx], steps[to]] = [steps[to], steps[idx]];
          redrawSteps(steps);
        }
        break;
      }
      case 'logout':
        await api('/api/logout', { method: 'POST' });
        showLogin();
        break;
      case 'tpl-cat': state.tplCat = btn.dataset.val; drawTemplateFilters(); drawTemplateGrid(); break;
      case 'tpl-os': state.tplOS = btn.dataset.val; drawTemplateFilters(); drawTemplateGrid(); break;
      case 'tpl-new': openModal('新建模板', templateForm(), saveFoot('tpl-form')); break;
      case 'tpl-view': {
        const t = state.templates.find(x => x.id === id);
        if (t) viewTemplate(t);
        break;
      }
      case 'tpl-edit': {
        const t = state.templates.find(x => x.id === id);
        if (t) openModal('编辑模板', templateForm(t), saveFoot('tpl-form'));
        break;
      }
      case 'tpl-copy': {
        const t = state.templates.find(x => x.id === id);
        if (t) openModal('复制为自定义模板', templateForm({ ...t, id: '', name: t.name + '（副本）' }), saveFoot('tpl-form'));
        break;
      }
      case 'tpl-del':
        if (confirm('确定删除该模板？')) {
          await api('/api/templates/' + id, { method: 'DELETE' });
          toast('模板已删除');
          state.templates = await api('/api/templates');
          drawTemplateFilters();
          drawTemplateGrid();
        }
        break;
      case 'tpl-clipboard': {
        const t = state.templates.find(x => x.id === id);
        if (t) await copyText(t.script);
        break;
      }
      case 'tpl-to-task': await taskFromTemplate(id); break;
      case 'tpl-pick': await openTemplatePicker(); break;
      case 'tpl-insert': {
        const t = state.templates.find(x => x.id === id);
        if (t) {
          const cur = collectSteps();
          const blank = cur.length === 1 && !cur[0].name.trim() && !cur[0].script.trim();
          redrawSteps(blank ? [{ name: t.name, script: t.script }] : [...cur, { name: t.name, script: t.script }]);
          closeModal();
          toast(`已添加步骤「${t.name}」`);
        }
        break;
      }
      case 'step-save-tpl': {
        const el = btn.closest('.step');
        const script = $('textarea', el).value;
        if (!script.trim()) { toast('步骤脚本为空，无法存为模板', 'err'); break; }
        const os = $('#task-form select[name="os_type"]').value;
        openModal('存为模板', templateForm({ name: $('input[name="step-name"]', el).value, script, os_type: os }), saveFoot('tpl-form'));
        break;
      }
    }
  } catch (err) { toast(err.message, 'err'); }
});

document.addEventListener('submit', e => {
  // 注意：表单内 name="id" 的控件会遮蔽 form.id 属性，必须用 getAttribute 读取。
  const fid = e.target.getAttribute('id');
  if (fid === 'host-form') { e.preventDefault(); submitHostForm(e.target); }
  else if (fid === 'task-form') { e.preventDefault(); submitTaskForm(e.target); }
  else if (fid === 'tpl-form') { e.preventDefault(); submitTemplateForm(e.target); }
  else if (fid === 'login-form') { e.preventDefault(); submitLogin(e.target); }
});

document.addEventListener('change', e => {
  if (e.target.id === 'host-auth') {
    const isKey = e.target.value === 'key';
    const editing = !!$('#host-form input[name="id"]').value;
    $('#secret-field').innerHTML = secretField(isKey, editing);
    $('#passphrase-field').hidden = !isKey;
  }
});

boot();
