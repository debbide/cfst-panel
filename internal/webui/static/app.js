const state = {
  token: localStorage.getItem('cfst_token') || '',
  settings: null,
  status: null,
  page: 'dashboard',
  selectedTaskId: '',
};

const $ = (sel) => document.querySelector(sel);
const $$ = (sel) => Array.from(document.querySelectorAll(sel));

const pageMeta = {
  dashboard: ['总览', '查看服务状态、最近测速与解析结果'],
  settings: ['完整配置', '在面板中完成系统、Cloudflare、测速、策略、定时与通知配置'],
  records: ['DNS 记录', '管理需要自动解析到优选 IP 的域名记录'],
  tasks: ['任务历史', '查看测速结果、优选 IP 与执行日志'],
  logs: ['运行日志', '查看系统运行与任务日志'],
};

async function api(path, options = {}) {
  const headers = Object.assign({ 'Content-Type': 'application/json' }, options.headers || {});
  if (state.token) headers.Authorization = `Bearer ${state.token}`;
  const res = await fetch(path, { ...options, headers, credentials: 'same-origin' });
  const text = await res.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = { error: text }; }
  if (!res.ok) {
    const msg = (data && data.error) || res.statusText || 'request failed';
    const err = new Error(msg);
    err.status = res.status;
    throw err;
  }
  return data;
}

function toast(msg, type = 'ok') {
  const el = $('#toast');
  el.textContent = msg;
  el.classList.remove('hidden', 'ok', 'err');
  el.classList.add(type === 'ok' ? 'ok' : 'err');
  clearTimeout(toast._t);
  toast._t = setTimeout(() => el.classList.add('hidden'), 3200);
}

function fmtTime(v) {
  if (!v) return '-';
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return String(v);
  return d.toLocaleString();
}

function fmtDuration(sec) {
  sec = Number(sec || 0);
  const h = Math.floor(sec / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = sec % 60;
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m ${s}s`;
  return `${s}s`;
}

function badge(status) {
  const cls = ['success', 'failed', 'running', 'pending', 'error', 'warn', 'info'].includes(status) ? status : 'info';
  return `<span class="badge ${cls}">${status || '-'}</span>`;
}

function setAuthView(authed) {
  $('#login-view').classList.toggle('hidden', authed);
  $('#main-view').classList.toggle('hidden', !authed);
}

function switchPage(page) {
  state.page = page;
  $$('.nav-item').forEach((btn) => btn.classList.toggle('active', btn.dataset.page === page));
  $$('.page').forEach((el) => el.classList.add('hidden'));
  $(`#page-${page}`).classList.remove('hidden');
  const [title, subtitle] = pageMeta[page];
  $('#page-title').textContent = title;
  $('#page-subtitle').textContent = subtitle;
}

function fillForm(form, data) {
  Array.from(form.elements).forEach((el) => {
    if (!el.name) return;
    const val = data[el.name];
    if (el.type === 'checkbox') {
      el.checked = Boolean(val);
    } else if (val !== undefined && val !== null) {
      el.value = val;
    } else {
      el.value = '';
    }
  });
}

function readForm(form) {
  const data = {};
  Array.from(form.elements).forEach((el) => {
    if (!el.name) return;
    if (el.type === 'checkbox') {
      data[el.name] = el.checked;
      return;
    }
    if (el.type === 'number') {
      data[el.name] = el.value === '' ? 0 : Number(el.value);
      return;
    }
    data[el.name] = el.value;
  });
  return data;
}

function renderLastTask(task) {
  const box = $('#last-task');
  if (!task) {
    box.className = 'kv-list empty';
    box.textContent = '暂无任务';
    return;
  }
  box.className = 'kv-list';
  box.innerHTML = [
    ['状态', badge(task.status)],
    ['触发', task.trigger],
    ['优选 IP', task.selected_ip || '-'],
    ['延迟', task.selected_latency ? `${task.selected_latency} ms` : '-'],
    ['速度', task.selected_speed ? `${task.selected_speed} MB/s` : '-'],
    ['丢包', `${task.selected_loss ?? 0} %`],
    ['更新记录数', task.updated_count ?? 0],
    ['开始', fmtTime(task.started_at)],
    ['结束', fmtTime(task.finished_at)],
    ['消息', task.message || '-'],
  ].map(([k, v]) => `<div class="kv-row"><span>${k}</span><div>${v}</div></div>`).join('');
}

function renderStatus(st) {
  state.status = st;
  $('#version-text').textContent = `v${st.version || '0.1.0'}`;
  $('#stat-uptime').textContent = fmtDuration(st.uptime_sec);
  $('#stat-listen').textContent = st.listen_addr || '-';
  $('#stat-cfst').textContent = st.cfst_binary_ok ? '可用' : '未找到';
  $('#stat-cfst-path').textContent = st.cfst_binary_path || '-';
  const meta = $('#stat-cfst-meta');
  if (meta) {
    const bits = [];
    if (st.cfst_bundled) bits.push('已内置');
    if (st.platform) bits.push(st.platform);
    meta.textContent = bits.length ? bits.join(' · ') : '-';
  }
  $('#stat-schedule').textContent = st.schedule_enabled ? '已启用' : '未启用';
  $('#stat-next-run').textContent = st.next_run_at ? `下次: ${fmtTime(st.next_run_at)}` : (st.cron_expr || '-');
  $('#stat-records').textContent = String(st.record_count ?? 0);
  $('#stat-running').textContent = st.running_task_id ? `运行中: ${st.running_task_id.slice(0, 8)}…` : '当前空闲';
  renderLastTask(st.last_task);
}

function renderRecords(list) {
  const body = $('#records-body');
  if (!list.length) {
    body.innerHTML = `<tr><td colspan="4" class="muted">暂无 DNS 记录，请先添加</td></tr>`;
    return;
  }
  body.innerHTML = list.map((r) => `
    <tr>
      <td>
        <strong>${escapeHtml(r.name)}</strong><br/>
        <span class="muted">${escapeHtml(r.type)} / TTL ${r.ttl}</span>
      </td>
      <td>${escapeHtml(r.content || '-')}</td>
      <td>${r.enabled ? '<span class="badge success">启用</span>' : '<span class="badge pending">停用</span>'}</td>
      <td>
        <button class="btn small" data-edit-record='${escapeAttr(JSON.stringify(r))}'>编辑</button>
        <button class="btn small danger" data-del-record="${r.id}">删除</button>
      </td>
    </tr>
  `).join('');
}

function renderTasks(list) {
  const body = $('#tasks-body');
  if (!list.length) {
    body.innerHTML = `<tr><td colspan="8" class="muted">暂无任务</td></tr>`;
    return;
  }
  body.innerHTML = list.map((t) => `
    <tr>
      <td>${fmtTime(t.created_at)}</td>
      <td>${badge(t.status)}</td>
      <td>${escapeHtml(t.trigger)}</td>
      <td>${escapeHtml(t.selected_ip || '-')}</td>
      <td>${t.selected_latency || '-'}</td>
      <td>${t.selected_speed || '-'}</td>
      <td>${t.updated_count ?? 0}</td>
      <td><button class="btn small" data-task-id="${t.id}">查看</button></td>
    </tr>
  `).join('');
}

function renderLogs(list) {
  const body = $('#logs-body');
  if (!list.length) {
    body.innerHTML = `<tr><td colspan="4" class="muted">暂无日志</td></tr>`;
    return;
  }
  body.innerHTML = list.map((e) => `
    <tr>
      <td>${fmtTime(e.created_at)}</td>
      <td>${badge(e.level)}</td>
      <td>${escapeHtml(e.source)}</td>
      <td>${escapeHtml(e.message)}</td>
    </tr>
  `).join('');
}

function renderTaskDetail(task) {
  if (task && task.id) {
    state.selectedTaskId = task.id;
  }
  $('#task-detail-id').textContent = task.id || '';
  const box = $('#task-detail');
  box.className = 'kv-list';
  let results = [];
  try { results = task.result_json ? JSON.parse(task.result_json) : []; } catch {}
  box.innerHTML = [
    ['状态', badge(task.status)],
    ['触发', task.trigger],
    ['优选 IP', task.selected_ip || '-'],
    ['延迟', task.selected_latency ?? '-'],
    ['速度', task.selected_speed ?? '-'],
    ['丢包', task.selected_loss ?? '-'],
    ['更新数', task.updated_count ?? 0],
    ['消息', escapeHtml(task.message || '-')],
    ['结果数', results.length],
  ].map(([k, v]) => `<div class="kv-row"><span>${k}</span><div>${v}</div></div>`).join('');

  const top = results.slice(0, 10).map((r, i) =>
    `${String(i + 1).padStart(2, '0')}. ${r.ip}  latency=${r.latency}  speed=${r.speed}  loss=${r.loss}`
  ).join('\n');
  $('#task-log').textContent = [
    top ? `Top results:\n${top}` : 'No parsed results',
    '',
    '---- log ----',
    task.log_text || '-',
  ].join('\n');
}

function escapeHtml(str) {
  return String(str ?? '')
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;');
}

function escapeAttr(str) {
  return escapeHtml(str).replaceAll("'", '&#39;');
}

async function loadSettings() {
  const settings = await api('/api/settings');
  state.settings = settings;
  fillForm($('#settings-form'), settings);
}

async function loadDashboard() {
  const st = await api('/api/status');
  renderStatus(st);
}

async function loadRecords() {
  const list = await api('/api/records');
  renderRecords(list);
}

async function loadTasks() {
  const list = await api('/api/tasks?limit=50');
  renderTasks(list);
  if (state.selectedTaskId) {
    try {
      const task = await api(`/api/tasks/${state.selectedTaskId}`);
      renderTaskDetail(task);
    } catch {}
  }
}

async function loadLogs() {
  const level = $('#log-level').value;
  const q = level ? `?limit=200&level=${encodeURIComponent(level)}` : '?limit=200';
  const list = await api(`/api/logs${q}`);
  renderLogs(list);
}

async function refreshAll() {
  await loadDashboard();
  if (state.page === 'settings') await loadSettings();
  if (state.page === 'records') await loadRecords();
  if (state.page === 'tasks') await loadTasks();
  if (state.page === 'logs') await loadLogs();
}

async function ensureSession() {
  if (!state.token) {
    setAuthView(false);
    return false;
  }
  try {
    await loadDashboard();
    await loadSettings();
    setAuthView(true);
    switchPage('dashboard');
    return true;
  } catch (err) {
    state.token = '';
    localStorage.removeItem('cfst_token');
    setAuthView(false);
    return false;
  }
}

function bindEvents() {
  $('#login-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    $('#login-error').textContent = '';
    try {
      const body = {
        username: $('#login-user').value.trim(),
        password: $('#login-pass').value,
      };
      const res = await api('/api/login', { method: 'POST', body: JSON.stringify(body) });
      state.token = res.token;
      localStorage.setItem('cfst_token', res.token);
      await ensureSession();
      toast('登录成功');
    } catch (err) {
      $('#login-error').textContent = err.message;
    }
  });

  $('#btn-logout').addEventListener('click', async () => {
    try { await api('/api/logout', { method: 'POST', body: '{}' }); } catch {}
    state.token = '';
    localStorage.removeItem('cfst_token');
    setAuthView(false);
  });

  $$('.nav-item').forEach((btn) => {
    btn.addEventListener('click', async () => {
      switchPage(btn.dataset.page);
      try {
        if (btn.dataset.page === 'settings') await loadSettings();
        if (btn.dataset.page === 'records') await loadRecords();
        if (btn.dataset.page === 'tasks') await loadTasks();
        if (btn.dataset.page === 'logs') await loadLogs();
        if (btn.dataset.page === 'dashboard') await loadDashboard();
      } catch (err) {
        toast(err.message, 'err');
      }
    });
  });

  $('#btn-refresh').addEventListener('click', async () => {
    try {
      await refreshAll();
      toast('已刷新');
    } catch (err) {
      toast(err.message, 'err');
    }
  });

  $('#btn-run-task').addEventListener('click', async () => {
    try {
      const task = await api('/api/tasks/run', { method: 'POST', body: '{}' });
      state.selectedTaskId = task.id;
      toast(`任务已启动: ${task.id.slice(0, 8)}… 延迟测速通常需要几分钟，请稍候`);
      switchPage('tasks');
      renderTaskDetail(task);
      await loadTasks();
    } catch (err) {
      toast(err.message, 'err');
    }
  });
  $('#btn-cancel-task').addEventListener('click', async () => {
    try {
      await api('/api/tasks/cancel', { method: 'POST', body: '{}' });
      toast('已发送取消请求');
      await loadDashboard();
    } catch (err) {
      toast(err.message, 'err');
    }
  });

  const testCF = async () => {
    try {
      const res = await api('/api/test/cloudflare', { method: 'POST', body: '{}' });
      $('#quick-check').textContent = JSON.stringify(res, null, 2);
      toast(`Cloudflare 连接成功: ${res.zone_name || res.zone_id}`);
    } catch (err) {
      $('#quick-check').textContent = err.message;
      toast(err.message, 'err');
    }
  };
  $('#btn-test-cf').addEventListener('click', testCF);
  $('#btn-settings-test-cf').addEventListener('click', testCF);

  $('#settings-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    try {
      const payload = readForm(e.target);
      // keep numeric floats correct
      payload.min_speed_mbps = Number(payload.min_speed_mbps || 0);
      payload.max_loss_percent = Number(payload.max_loss_percent || 0);
      const saved = await api('/api/settings', { method: 'PUT', body: JSON.stringify(payload) });
      state.settings = saved;
      fillForm(e.target, saved);
      await loadDashboard();
      toast('配置已保存');
    } catch (err) {
      toast(err.message, 'err');
    }
  });

  $('#btn-reload-settings').addEventListener('click', async () => {
    try {
      await loadSettings();
      toast('配置已重新加载');
    } catch (err) {
      toast(err.message, 'err');
    }
  });

  $('#record-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    try {
      const payload = readForm(e.target);
      payload.ttl = Number(payload.ttl || 1);
      payload.enabled = Boolean(payload.enabled);
      payload.proxied = false; // preferred-IP mode always DNS only
      const method = payload.id ? 'PUT' : 'POST';
      const path = payload.id ? `/api/records/${payload.id}` : '/api/records';
      await api(path, { method, body: JSON.stringify(payload) });
      e.target.reset();
      e.target.elements.enabled.checked = true;
      await loadRecords();
      toast('DNS 记录已保存');
    } catch (err) {
      toast(err.message, 'err');
    }
  });

  $('#btn-reset-record').addEventListener('click', () => {
    const form = $('#record-form');
    form.reset();
    form.elements.id.value = '';
    form.elements.enabled.checked = true;
  });

  $('#records-body').addEventListener('click', async (e) => {
    const editBtn = e.target.closest('[data-edit-record]');
    if (editBtn) {
      const rec = JSON.parse(editBtn.getAttribute('data-edit-record'));
      fillForm($('#record-form'), rec);
      return;
    }
    const delBtn = e.target.closest('[data-del-record]');
    if (delBtn) {
      if (!confirm('确认删除该 DNS 记录？')) return;
      try {
        await api(`/api/records/${delBtn.getAttribute('data-del-record')}`, { method: 'DELETE' });
        await loadRecords();
        toast('已删除');
      } catch (err) {
        toast(err.message, 'err');
      }
    }
  });

  $('#tasks-body').addEventListener('click', async (e) => {
    const btn = e.target.closest('[data-task-id]');
    if (!btn) return;
    try {
      const task = await api(`/api/tasks/${btn.getAttribute('data-task-id')}`);
      renderTaskDetail(task);
    } catch (err) {
      toast(err.message, 'err');
    }
  });

  $('#log-level').addEventListener('change', async () => {
    try { await loadLogs(); } catch (err) { toast(err.message, 'err'); }
  });

  $('#btn-clear-logs').addEventListener('click', async () => {
    if (!confirm('确认清空全部日志？')) return;
    try {
      await api('/api/logs', { method: 'DELETE' });
      await loadLogs();
      toast('日志已清空');
    } catch (err) {
      toast(err.message, 'err');
    }
  });
}

async function boot() {
  bindEvents();
  await ensureSession();
  setInterval(async () => {
    if (!state.token) return;
    try {
      if (state.page === 'dashboard' || state.page === 'tasks') {
        await loadDashboard();
        if (state.page === 'tasks') await loadTasks();
      } else if (state.selectedTaskId) {
        // Keep a selected running task fresh even if user left the page briefly.
        const task = await api(`/api/tasks/${state.selectedTaskId}`);
        if (task.status === 'running') {
          renderTaskDetail(task);
        }
      }
    } catch {}
  }, 2000);
}

boot();
