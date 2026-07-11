/* ============================================================
   TempMail SPA — 主应用逻辑
   ============================================================ */

'use strict';

// ─── 配置 ───────────────────────────────────────────────────
const API_BASE = '/api';
const PUBLIC_BASE = '/public';

// ─── 状态 ───────────────────────────────────────────────────
const state = {
  apiKey:    localStorage.getItem('tm_apikey') || '',
  account:   JSON.parse(localStorage.getItem('tm_account') || 'null'),
  theme:     localStorage.getItem('tm_theme') || 'light',
  page:      'dashboard',
  // 当前邮箱
  currentMailbox: null,
  currentEmail:   null,
  // 缓存
  mailboxes: [],
  emails:    [],
  adminDomainQuery: '',
  adminDomainStatus: 'all',
  adminDomainHostname: 'all',
  adminDomainSelection: {},
  mailboxPageSize: 6,
  adminOTPShare: {
    mailboxes: [],
    mailbox: null,
    share: null,
    address: '',
    token: '',
  },
};

// ─── 工具函数 ───────────────────────────────────────────────
const $ = id => document.getElementById(id);
const el = (tag, cls, html) => {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (html !== undefined) e.innerHTML = html;
  return e;
};
const DASH_MOBILE_BREAKPOINT = 768;
const DASH_MOBILE_VIEWS = new Set(['mailboxes', 'emails', 'detail']);
const DEFAULT_MAILBOX_PAGE_SIZE = 6;
const MIN_MAILBOX_PAGE_SIZE = 1;
const MAX_MAILBOX_PAGE_SIZE = 24;

function isMobileLayout() {
  return window.matchMedia(`(max-width: ${DASH_MOBILE_BREAKPOINT}px)`).matches;
}

function buildDataLabel(label, content, attrs = '') {
  const suffix = attrs ? ' ' + attrs : '';
  return `<td data-label="${escHtml(label)}"${suffix}>${content}</td>`;
}

function normalizeHostnameList(items) {
  return Array.isArray(items)
    ? items
        .filter(item => item && String(item.hostname || '').trim())
        .map(item => ({
          ...item,
          id: Number(item.id) || 0,
          hostname: String(item.hostname || '').trim(),
          is_active: item.is_active !== false,
          domain_count: Number(item.domain_count) || 0,
        }))
    : [];
}

function findHostnameById(items, id) {
  const target = Number(id) || 0;
  return normalizeHostnameList(items).find(item => item.id === target) || null;
}

function buildHostnameOptions(items, selectedId, opts = {}) {
  const hostnames = normalizeHostnameList(items);
  const selected = selectedId === null || selectedId === undefined || selectedId === '' ? '' : String(Number(selectedId) || 0);
  const blank = opts.allowBlank
    ? `<option value="">${escHtml(opts.blankLabel || '不指定')}</option>`
    : '';
  return blank + hostnames.map(item => {
    const value = String(item.id);
    const label = item.hostname + (item.is_active ? '' : '（停用）');
    return `<option value="${value}" ${selected === value ? 'selected' : ''}>${escHtml(label)}</option>`;
  }).join('');
}

function getDefaultHostnameFromSettings(settings) {
  return String(settings?.smtp_hostname || '').trim();
}

function getActiveHostnamesFromSettings(settings) {
  return normalizeHostnameList(settings?.hostnames || []);
}

function toast(msg, type = 'info') {
  const icons = { success: '✓', error: '✗', warn: '⚠', info: 'ℹ' };
  const t = el('div', `toast ${type}`, `<span>${icons[type]||'ℹ'}</span><span>${escHtml(msg)}</span>`);
  const c = $('toast-container');
  c.appendChild(t);
  setTimeout(() => { t.style.opacity = '0'; t.style.transition = 'opacity 0.3s'; setTimeout(() => t.remove(), 300); }, 3500);
}

function escHtml(s) {
  return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}

function formatDate(s) {
  if (!s) return '—';
  const d = new Date(s);
  return d.toLocaleString('zh-CN', { month:'2-digit', day:'2-digit', hour:'2-digit', minute:'2-digit'});
}

function formatBytes(bytes) {
  const value = Number(bytes) || 0;
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1).replace(/\.0$/, '')} KB`;
  return `${(value / (1024 * 1024)).toFixed(1).replace(/\.0$/, '')} MB`;
}

function clampMailboxPageSize(value) {
  const num = Math.floor(Number(value) || DEFAULT_MAILBOX_PAGE_SIZE);
  return Math.max(MIN_MAILBOX_PAGE_SIZE, Math.min(MAX_MAILBOX_PAGE_SIZE, num));
}

function getMailboxPageSize() {
  return clampMailboxPageSize(state.mailboxPageSize);
}

function applyPublicSettings(settings = {}) {
  if (settings && settings.mailbox_page_size !== undefined && settings.mailbox_page_size !== null && settings.mailbox_page_size !== '') {
    state.mailboxPageSize = clampMailboxPageSize(settings.mailbox_page_size);
  }
}

function timeAgo(s) {
  if (!s) return '—';
  const diff = Date.now() - new Date(s).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return '刚刚';
  if (mins < 60) return `${mins}分钟前`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}小时前`;
  return `${Math.floor(hrs/24)}天前`;
}

async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text);
    toast('已复制到剪贴板', 'success');
  } catch {
    toast('复制失败，请手动选择', 'warn');
  }
}

function parseContentDispositionFilename(header) {
  if (!header) return '';
  const utfMatch = header.match(/filename\*\s*=\s*UTF-8''([^;]+)/i);
  if (utfMatch) {
    try { return decodeURIComponent(utfMatch[1]); } catch (_) { return utfMatch[1]; }
  }
  const quotedMatch = header.match(/filename\s*=\s*"([^"]+)"/i);
  if (quotedMatch) return quotedMatch[1];
  const plainMatch = header.match(/filename\s*=\s*([^;]+)/i);
  return plainMatch ? plainMatch[1].trim() : '';
}

function triggerBlobDownload(blob, filename) {
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename || 'attachment';
  document.body.appendChild(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

function buildEmailAttachments(mailboxId, emailId, attachments) {
  const items = Array.isArray(attachments) ? attachments : [];
  if (!items.length) return '';
  return `
    <div class="email-attachments">
      <div class="email-attachments-head">
        <span class="email-attachments-title">附件 ${items.length}</span>
        <span class="email-attachments-hint">点击下载</span>
      </div>
      <div class="email-attachments-list">
        ${items.map(att => `
          <div class="attachment-item">
            <div class="attachment-main">
              <div class="attachment-name" title="${escHtml(att.filename || ('attachment-' + att.id))}">${escHtml(att.filename || ('attachment-' + att.id))}</div>
              <div class="attachment-meta">
                <span>${formatBytes(att.size_bytes || 0)}</span>
                <span>${escHtml(att.content_type || 'application/octet-stream')}</span>
                ${att.inline ? '<span class="attachment-badge">内联</span>' : ''}
              </div>
            </div>
            <button class="btn btn-ghost btn-sm" onclick="downloadEmailAttachment('${mailboxId}','${emailId}',${Number(att.id) || 0})">下载</button>
          </div>
        `).join('')}
      </div>
    </div>
  `;
}

function isMailboxForwardEnabled(mailbox) {
  return !!mailbox?.tg_forward_enabled;
}

function buildMailboxForwardBadge(mailbox) {
  return isMailboxForwardEnabled(mailbox) ? '<span class="mailbox-forward-badge">✈ TG 转发</span>' : '';
}

function buildMailboxForwardButton(mailbox, stopPropagation = false) {
  const enabled = isMailboxForwardEnabled(mailbox);
  const clickPrefix = stopPropagation ? 'event.stopPropagation();' : '';
  const btnClass = enabled ? 'btn btn-primary btn-sm' : 'btn btn-ghost btn-sm';
  const next = enabled ? 'false' : 'true';
  const title = enabled ? '关闭 Telegram 转发' : '开启 Telegram 转发';
  return `<button class="${btnClass}" onclick="${clickPrefix}toggleMailboxForward('${mailbox.id}',${next})" title="${title}">✈ ${enabled ? '开' : '关'}</button>`;
}

function syncMailboxState(updated) {
  if (!updated || !updated.id) return;
  if (Array.isArray(dashState?.mailboxes)) {
    dashState.mailboxes = dashState.mailboxes.map(mb => mb.id === updated.id ? updated : mb);
  }
  if (Array.isArray(state.mailboxes)) {
    state.mailboxes = state.mailboxes.map(mb => mb.id === updated.id ? updated : mb);
  }
  if (dashState?.selectedMailbox?.id === updated.id) {
    dashState.selectedMailbox = updated;
  }
  if (state.currentMailbox?.id === updated.id) {
    state.currentMailbox = { ...state.currentMailbox, ...updated };
  }
}

function buildDashboardMailboxHeaderActions(mailbox) {
  const mbIsFav = !!mailbox.is_favorite;
  const mbFavIcon = mbIsFav ? '★' : '☆';
  const mbFavTitle = mbIsFav ? '取消收藏' : '收藏（防过期）';
  return `
    ${buildMailboxForwardButton(mailbox)}
    <button class="btn btn-ghost btn-sm" onclick="toggleFavorite('${mailbox.id}',${!mbIsFav})" title="${mbFavTitle}">${mbFavIcon}</button>
    <button class="btn btn-ghost btn-sm" onclick="copyText('${escHtml(mailbox.full_address)}')" title="复制地址">⎘</button>
    <button class="btn btn-ghost btn-sm" onclick="paneRenderEmails()" title="刷新">↻</button>
    <button class="btn btn-danger btn-sm" onclick="confirmDeleteMailbox('${mailbox.id}','${escHtml(mailbox.full_address)}')" title="删除邮箱">✕</button>
  `;
}

function renderInboxTopbarActions(mailbox) {
  const actions = $('topbar-actions');
  if (!actions || !mailbox) return;
  actions.innerHTML = `
    ${buildMailboxForwardButton(mailbox)}
    <button class="btn btn-ghost btn-sm" onclick="copyText('${escHtml(mailbox.full_address)}')" style="margin-left:0.4rem">⎘ 复制地址</button>
    <button class="btn btn-primary btn-sm" onclick="refreshInbox()" style="margin-left:0.4rem">↻ 刷新</button>
    <button class="btn btn-ghost btn-sm" onclick="navigate('dashboard')" style="margin-left:0.4rem">← 返回</button>
  `;
}

// ─── API 客户端 ─────────────────────────────────────────────
async function apiFetch(path, opts = {}) {
  const headers = { 'Content-Type': 'application/json', ...(opts.headers || {}) };
  if (state.apiKey) headers['Authorization'] = `Bearer ${state.apiKey}`;
  const res = await fetch(path, { ...opts, headers });
  let data;
  try { data = await res.json(); } catch { data = {}; }
  if (!res.ok) {
    const errMsg = data.error || data.message || `HTTP ${res.status}`;
    throw new Error(errMsg);
  }
  return data;
}

const api = {
  // 公共
  publicSettings: () => fetch(PUBLIC_BASE + '/settings').then(r => r.json()),
  publicStats:     () => fetch(PUBLIC_BASE + '/stats').then(r => r.json()),
  register: body  => apiFetch(PUBLIC_BASE + '/register', { method: 'POST', body: JSON.stringify(body) }),

  // 账户
  me:              () => apiFetch(API_BASE + '/me'),
  stats:           () => apiFetch(API_BASE + '/stats'),
  domainsPayload:  (params = '') => apiFetch(API_BASE + '/domains' + (params ? '?' + params : '')),
  hostnamesPayload: () => apiFetch(API_BASE + '/hostnames'),
  hostnames:       () => api.hostnamesPayload().then(d => Array.isArray(d) ? d : (d.hostnames || [])),
  // 域名 → 解包 {domains:[...]} → 数组
  domains:         (params = '') => api.domainsPayload(params).then(d => Array.isArray(d) ? d : (d.domains || [])),
  // 任意已登录用户提交域名 MX 验证
  submitDomain:    body => apiFetch(API_BASE + '/domains/submit', { method: 'POST', body: JSON.stringify(body) }),
  // 轮询域名状态（任意已登录用户，不需要管理员权限）
  getDomainStatus: id => apiFetch(API_BASE + '/domains/' + id + '/status'),
  // 邮箱 → 解包 {data:[...]}
  createMailbox:   (body) => apiFetch(API_BASE + '/mailboxes', { method: 'POST', body: JSON.stringify(body || {}) }).then(d => d.mailbox || d),
  listMailboxesPage: (page = 1, size = 20, folder = 'all') => {
    const folderParam = folder ? '&folder=' + encodeURIComponent(folder) : '';
    return apiFetch(API_BASE + '/mailboxes?page=' + page + '&size=' + size + folderParam).then(d => ({
      data: Array.isArray(d) ? d : (d.data || []),
      total: d.total ?? (Array.isArray(d) ? d.length : 0),
      page: d.page ?? page,
      size: d.size ?? size,
    }));
  },
  listMailboxes:   (folder = 'all') => api.listMailboxesPage(1, 20, folder).then(d => d.data),
  lookupMailboxByAddress: address => apiFetch(API_BASE + '/mailboxes/lookup?address=' + encodeURIComponent(address)).then(d => d.mailbox || d),
  deleteMailbox: id  => apiFetch(API_BASE + '/mailboxes/' + id, { method: 'DELETE' }),
  setMailboxForward: (id, enabled) => apiFetch(API_BASE + '/mailboxes/' + id + '/forward', {
    method: 'PUT',
    body: JSON.stringify({ enabled: !!enabled }),
  }).then(d => d.mailbox || d),
  latestOTP:      id => apiFetch(API_BASE + '/mailboxes/' + id + '/otp/latest').then(d => d.otp || d),
  getMailboxOTPShare: id => apiFetch(API_BASE + '/mailboxes/' + id + '/otp-share').then(d => d.share || d),
  upsertMailboxOTPShare: (id, body) => apiFetch(API_BASE + '/mailboxes/' + id + '/otp-share', {
    method: 'POST',
    body: JSON.stringify(body || {}),
  }).then(d => d.share || d),
  deleteMailboxOTPShare: id => apiFetch(API_BASE + '/mailboxes/' + id + '/otp-share', { method: 'DELETE' }),
  // 邮件 → 解包 {data:[...]}
  listEmailsPage: (mid, page = 1, size = 20) => apiFetch(API_BASE + '/mailboxes/' + mid + '/emails?page=' + page + '&size=' + size).then(d => ({
    data: Array.isArray(d) ? d : (d.data || []),
    total: d.total ?? (Array.isArray(d) ? d.length : 0),
    page: d.page ?? page,
    size: d.size ?? size,
  })),
  listEmails: mid    => api.listEmailsPage(mid).then(d => d.data),
  getEmail:   (mid, eid) => apiFetch(API_BASE + '/mailboxes/' + mid + '/emails/' + eid).then(d => d.email || d),
  forwardEmailToTG: (mid, eid) => apiFetch(API_BASE + '/mailboxes/' + mid + '/emails/' + eid + '/forward/tg', { method: 'POST' }),
  downloadEmailAttachment: async (mid, eid, aid) => {
    const headers = {};
    if (state.apiKey) headers['Authorization'] = `Bearer ${state.apiKey}`;
    const res = await fetch(API_BASE + '/mailboxes/' + mid + '/emails/' + eid + '/attachments/' + aid, { headers });
    if (!res.ok) {
      let data = {};
      try { data = await res.json(); } catch (_) {}
      throw new Error(data.error || data.message || `HTTP ${res.status}`);
    }
    return {
      blob: await res.blob(),
      filename: parseContentDispositionFilename(res.headers.get('Content-Disposition')),
    };
  },
  deleteEmail:(mid, eid) => apiFetch(API_BASE + '/mailboxes/' + mid + '/emails/' + eid, { method: 'DELETE' }),
  // 管理
  admin: {
    listAccounts:  (page=1,size=50) => apiFetch(API_BASE + '/admin/accounts?page='+page+'&size='+size).then(d => Array.isArray(d) ? d : (d.data || [])),
    createAccount: body => apiFetch(API_BASE + '/admin/accounts', { method: 'POST', body: JSON.stringify(body) }),
    deleteAccount: id   => apiFetch(API_BASE + '/admin/accounts/' + id, { method: 'DELETE' }),
    addDomain:   body => apiFetch(API_BASE + '/admin/domains', { method: 'POST', body: JSON.stringify(body) }),
    deleteDomain:  id => apiFetch(API_BASE + '/admin/domains/' + id, { method: 'DELETE' }),
    deleteDomainCF: id => apiFetch(API_BASE + '/admin/domains/' + id + '/cf', { method: 'DELETE' }),
    toggleDomain:  (id, active) => apiFetch(API_BASE + '/admin/domains/' + id + '/toggle', { method: 'PUT', body: JSON.stringify({ active }) }),
    updateDomainHostname: (id, body) => apiFetch(API_BASE + '/admin/domains/' + id + '/hostname', { method: 'PUT', body: JSON.stringify(body) }),
    updateDomainSubdomain: (id, body) => apiFetch(API_BASE + '/admin/domains/' + id + '/subdomain', { method: 'PUT', body: JSON.stringify(body) }),
    batchToggleDomains: (ids, active) => apiFetch(API_BASE + '/admin/domains/batch/toggle', { method: 'PUT', body: JSON.stringify({ ids, active }) }),
    batchDeleteDomains: (ids, delete_cloudflare) => apiFetch(API_BASE + '/admin/domains/batch/delete', { method: 'PUT', body: JSON.stringify({ ids, delete_cloudflare }) }),
    batchToggleDomainsSubdomain: (ids, enabled) => apiFetch(API_BASE + '/admin/domains/batch/subdomain', { method: 'PUT', body: JSON.stringify({ ids, enabled }) }),
    cfCreateDomain: body => apiFetch(API_BASE + '/admin/domains/cf-create', { method: 'POST', body: JSON.stringify(body) }),
    listHostnames:  () => apiFetch(API_BASE + '/admin/hostnames').then(d => Array.isArray(d) ? d : (d.hostnames || [])),
    addHostname:    body => apiFetch(API_BASE + '/admin/hostnames', { method: 'POST', body: JSON.stringify(body) }).then(d => d.hostname || d),
    updateHostname: (id, body) => apiFetch(API_BASE + '/admin/hostnames/' + id, { method: 'PUT', body: JSON.stringify(body) }).then(d => d.hostname || d),
    toggleHostname: (id, active) => apiFetch(API_BASE + '/admin/hostnames/' + id + '/toggle', { method: 'PUT', body: JSON.stringify({ active }) }),
    deleteHostname: id => apiFetch(API_BASE + '/admin/hostnames/' + id, { method: 'DELETE' }),
    getSettings:    () => apiFetch(API_BASE + '/admin/settings'),
    saveSettings: body => apiFetch(API_BASE + '/admin/settings', { method: 'PUT', body: JSON.stringify(body) }),
    testTelegram: () => apiFetch(API_BASE + '/admin/settings/tg/test', { method: 'POST' }),
    mxImport:    body => apiFetch(API_BASE + '/admin/domains/mx-import', { method: 'POST', body: JSON.stringify(body) }),
    mxRegister:  body => apiFetch(API_BASE + '/admin/domains/mx-register', { method: 'POST', body: JSON.stringify(body) }),
    getDomainStatus: id => apiFetch(API_BASE + '/admin/domains/' + id + '/status'),
  },
};

// ─── 主题 ────────────────────────────────────────────────────
function applyTheme(t) {
  document.documentElement.dataset.theme = t;
  state.theme = t;
  localStorage.setItem('tm_theme', t);
  const btn = $('btn-theme');
  if (btn) btn.textContent = t === 'dark' ? '☀ 浅色' : '☾ 深色';
}

// ─── 认证 ─────────────────────────────────────────────────────
async function tryLogin(key) {
  state.apiKey = key;
  try {
    const acct = await apiFetch(API_BASE + '/me');
    state.account = acct;
    localStorage.setItem('tm_apikey', key);
    localStorage.setItem('tm_account', JSON.stringify(acct));
    showMainLayout();
    navigate('dashboard');
    toast(`欢迎回来，${acct.username || '用户'}`, 'success');
  } catch (e) {
    state.apiKey = '';
    toast('API Key 无效: ' + e.message, 'error');
  }
}

function logout() {
  state.apiKey = '';
  state.account = null;
  localStorage.removeItem('tm_apikey');
  localStorage.removeItem('tm_account');
  showAuthPage();
}

// ─── 路由 ─────────────────────────────────────────────────────
function navigate(page, params = {}) {
  closeSidebar();
  // 离开收件箱时停止自动刷新
  if (page !== 'inbox') clearInboxPoller();
  // 离开 dashboard 时停止三栏邮件列表轮询
  if (page !== 'dashboard' && typeof stopEmailListPoller === 'function') stopEmailListPoller();
  state.page = page;
  Object.assign(state, params);
  renderPage(page);
  // 更新侧导航高亮
  document.querySelectorAll('.nav-item').forEach(n => {
    n.classList.toggle('active', n.dataset.page === page);
  });
}

// ─── 布局渲染 ──────────────────────────────────────────────────
function showAuthPage() {
  $('app').innerHTML = '';
  $('app').appendChild(buildAuthPage());
  renderLoginForm();
}

function showMainLayout() {
  $('app').innerHTML = '';
  $('app').appendChild(buildMainLayout());
  applyTheme(state.theme);
}

function buildAuthPage() {
  const wrap = el('div', null);
  wrap.id = 'auth-page';

  const card = el('div', 'auth-card');
  card.innerHTML = `
    <div class="auth-logo">
      <div class="logo-icon">✉</div>
      <h1>TempMail</h1>
      <p>临时邮箱服务 · 安全隔离 · 按需分配</p>
    </div>
    <div class="auth-tabs">
      <button class="auth-tab active" id="tab-login" onclick="switchAuthTab('login')">使用 API Key 登录</button>
      <button class="auth-tab" id="tab-reg" onclick="switchAuthTab('reg')">注册账户</button>
    </div>
    <div id="auth-form-area"></div>
  `;
  wrap.appendChild(card);

  // 检查是否允许注册
  api.publicSettings().then(d => {
    const open = d.registration_open === 'true' || d.registration_open === true;
    if (!open) {
      const regTab = card.querySelector('#tab-reg');
      if (regTab) { regTab.disabled = true; regTab.title = '管理员已关闭注册'; }
    }
  }).catch(() => {});

  return wrap;
}

window.switchAuthTab = function(t) {
  document.querySelectorAll('.auth-tab').forEach(b => b.classList.remove('active'));
  if (t === 'login') {
    $('tab-login').classList.add('active');
    renderLoginForm();
  } else {
    $('tab-reg').classList.add('active');
    renderRegForm();
  }
};

function renderLoginForm() {
  const area = $('auth-form-area');
  if (!area) return;
  area.innerHTML = `
    <div class="form-group">
      <label class="form-label">API Key</label>
      <input class="form-input" id="login-key" type="password" placeholder="tm_xxxxxxxxxxxx" autocomplete="current-password" />
      <div class="form-hint">在邮箱管理后台获取的 API Key</div>
    </div>
    <button class="btn btn-primary" style="width:100%" onclick="doLogin()">登 录</button>
    <div class="divider"></div>
    <div style="text-align:center;font-size:0.78rem;color:var(--text-muted)">
      没有账户？联系管理员创建，或点击上方"注册账户"
    </div>
  `;
  const inp = $('login-key');
  if (inp) inp.addEventListener('keydown', e => { if (e.key === 'Enter') doLogin(); });
}

function renderRegForm() {
  const area = $('auth-form-area');
  if (!area) return;
  area.innerHTML = `
    <div class="form-group">
      <label class="form-label">用户名</label>
      <input class="form-input" id="reg-username" type="text" placeholder="your_name" />
    </div>
    <div class="form-group">
      <label class="form-label">邮箱（可选）</label>
      <input class="form-input" id="reg-email" type="email" placeholder="contact@example.com" />
    </div>
    <button class="btn btn-primary" style="width:100%" onclick="doRegister()">注 册</button>
  `;
}

window.doLogin = async function() {
  const key = ($('login-key')?.value || '').trim();
  if (!key) { toast('请输入 API Key', 'warn'); return; }
  await tryLogin(key);
};

window.doRegister = async function() {
  const username = ($('reg-username')?.value || '').trim();
  const email    = ($('reg-email')?.value || '').trim();
  if (!username) { toast('请输入用户名', 'warn'); return; }
  try {
    const result = await api.register({ username, email: email || undefined });
    // 显示成功
    const area = $('auth-form-area');
    area.innerHTML = `
      <div class="apikey-hero">
        <span class="big-icon">🎉</span>
        <h2>注册成功！</h2>
        <p>请保存您的 API Key，它不会再次显示。</p>
        <div class="code-box">
          <span id="new-key">${escHtml(result.api_key)}</span>
          <button class="copy-btn" onclick="copyText('${escHtml(result.api_key)}')" title="复制">⎘</button>
        </div>
        <button class="btn btn-success" style="margin-top:1.2rem;width:100%" onclick="tryLogin('${escHtml(result.api_key)}')">立即登录</button>
      </div>
    `;
  } catch(e) {
    toast('注册失败: ' + e.message, 'error');
  }
};

// ─── 主布局 ────────────────────────────────────────────────────
function buildMainLayout() {
  const layout = el('div', null);
  layout.id = 'main-layout';
  layout.style.display = 'flex';
  layout.style.flex = '1';

  const isAdmin = state.account?.is_admin;
  const username = state.account?.username || '用户';

  // sidebar
  layout.innerHTML = `
    <div class="sidebar-backdrop" id="sidebar-backdrop" onclick="closeSidebar()"></div>
    <nav class="sidebar" id="main-sidebar">
      <div class="sidebar-logo">
        <div class="logo-mark">✉</div>
        <div>
          <span>TempMail</span>
          <small>临时邮箱服务</small>
        </div>
      </div>
      <div class="sidebar-nav">
        <div class="nav-section">邮件</div>
        <button class="nav-item active" data-page="dashboard" onclick="navigate('dashboard')">
          <span class="nav-icon">⊞</span><span>邮箱总览</span>
        </button>
        <button class="nav-item" data-page="domains-guide" onclick="navigate('domains-guide')">
          <span class="nav-icon">◎</span><span>域名列表</span>
        </button>
        <button class="nav-item" data-page="api-docs" onclick="navigate('api-docs')">
          <span class="nav-icon">📖</span><span>API 文档</span>
        </button>
        ${isAdmin ? `
        <div class="nav-section">管理</div>
        <button class="nav-item" data-page="admin-accounts" onclick="navigate('admin-accounts')">
          <span class="nav-icon">👥</span><span>账户管理</span>
        </button>
        <button class="nav-item" data-page="admin-domains" onclick="navigate('admin-domains')">
          <span class="nav-icon">🌐</span><span>域名管理</span>
        </button>
        <button class="nav-item" data-page="admin-settings" onclick="navigate('admin-settings')">
          <span class="nav-icon">⚙</span><span>系统设置</span>
        </button>
        ` : ''}
      </div>
      <div class="sidebar-bottom">
        <div class="user-chip">
          <div class="user-avatar">${username.charAt(0).toUpperCase()}</div>
          <div class="user-chip-info">
            <div class="user-chip-name">${escHtml(username)}</div>
            <div class="user-chip-role">${isAdmin ? '管理员' : '普通用户'}</div>
          </div>
        </div>
        <button class="btn-logout" onclick="logout()">⏏ 退出登录</button>
        <button class="btn-theme" id="btn-theme" onclick="toggleTheme()">${state.theme==='dark'?'☀ 浅色':'☾ 深色'}</button>
      </div>
    </nav>
    <div class="content" id="content-area">
      <div class="topbar">
        <div>
          <button class="hamburger-btn" id="hamburger-btn" onclick="toggleSidebar()" aria-label="菜单">☰</button>
          <div>
            <div class="topbar-title" id="topbar-title">邮箱总览</div>
            <div class="topbar-subtitle" id="topbar-subtitle"></div>
          </div>
        </div>
        <div id="topbar-actions"></div>
      </div>
      <div id="page-content" class="page"></div>
    </div>
  `;
  return layout;
}

window.toggleTheme = function() {
  applyTheme(state.theme === 'dark' ? 'light' : 'dark');
};
window.navigate = navigate;
window.logout   = logout;
window.copyText = copyText;
window.tryLogin = tryLogin;

window.toggleSidebar = function() {
  const sidebar  = document.getElementById('main-sidebar');
  const backdrop = document.getElementById('sidebar-backdrop');
  if (!sidebar) return;
  const isOpen = sidebar.classList.contains('mob-open');
  if (isOpen) {
    sidebar.classList.remove('mob-open');
    if (backdrop) backdrop.classList.remove('show');
  } else {
    sidebar.classList.add('mob-open');
    if (backdrop) backdrop.classList.add('show');
  }
};

window.closeSidebar = function() {
  const sidebar  = document.getElementById('main-sidebar');
  const backdrop = document.getElementById('sidebar-backdrop');
  if (sidebar)  sidebar.classList.remove('mob-open');
  if (backdrop) backdrop.classList.remove('show');
};

// ─── 页面渲染路由 ───────────────────────────────────────────
async function renderPage(page) {
  const container = $('page-content');
  if (!container) return;
  container.innerHTML = '<div style="padding:2rem;text-align:center"><span class="spinner"></span></div>';

  const titles = {
    'dashboard':      ['邮箱总览', '管理您的临时邮箱'],
    'inbox':          ['邮件列表', ''],
    'email-view':     ['邮件内容', ''],
    'domains-guide':  ['域名列表 & 添加指南', '查看可用域名并了解如何添加新域名'],
    'admin-accounts': ['账户管理', '创建和管理用户账户'],
    'admin-domains':  ['域名管理', '管理域名池'],
    'admin-settings': ['系统设置', ''],
    'apikey-show':    ['API Key', ''],
    'api-docs':       ['API 接口文档', '查看所有可用 API 及调用示例'],
  };
  const [t, s] = titles[page] || ['—', ''];
  const title = $('topbar-title'); if (title) title.textContent = t;
  const sub   = $('topbar-subtitle'); if (sub) sub.textContent = s;
  const actions = $('topbar-actions'); if (actions) actions.innerHTML = '';

  try {
    switch(page) {
      case 'dashboard':      await renderDashboard(container); break;
      case 'inbox':          await renderInbox(container); break;
      case 'email-view':     await renderEmailView(container); break;
      case 'domains-guide':  await renderDomainsGuide(container); break;
      case 'admin-accounts': await renderAdminAccounts(container); break;
      case 'admin-domains':  await renderAdminDomains(container); break;
      case 'admin-settings': await renderAdminSettings(container); break;
      case 'apikey-show':    renderApiKeyShow(container); break;
      case 'api-docs':       renderApiDocs(container); break;
      default: container.innerHTML = '<div class="page"><p>页面未找到</p></div>';
    }
  } catch(e) {
    container.innerHTML = `<div style="padding:2rem;color:var(--clr-danger)">加载失败：${escHtml(e.message)}</div>`;
  }
}

// ─── Dashboard（三栏：邮箱列表 | 邮件列表 | 邮件正文） ──────────────
const PAGE_SIZE = 20;
const MAILBOX_FOLDER_TEMP = 'temp';
const MAILBOX_FOLDER_FAVORITES = 'favorites';
const dashState = {
  mailboxes: [],
  mailboxTotal: 0,
  emails: [],
  emailTotal: 0,
  selectedMailbox: null, // {id, full_address, is_favorite, expires_at, ...}
  selectedEmailId: null,
  mailboxFolder: MAILBOX_FOLDER_TEMP,
  mailboxPage: 1,
  emailPage: 1,
  mobileView: 'mailboxes',
  emailListPoller: null,
};

function setMobileView(view) {
  if (!DASH_MOBILE_VIEWS.has(view)) return;
  dashState.mobileView = view;
  const grid = document.querySelector('.three-pane-grid');
  if (grid) grid.dataset.mobileView = view;
}

function normalizeDashboardMobileView() {
  let nextView = dashState.mobileView;
  if (!dashState.selectedMailbox) nextView = 'mailboxes';
  else if (!dashState.selectedEmailId && nextView === 'detail') nextView = 'emails';
  if (!DASH_MOBILE_VIEWS.has(nextView)) nextView = 'mailboxes';
  setMobileView(nextView);
}

function buildPaneBackButton(targetView, label) {
  return `
    <button class="pane-back-btn" type="button" onclick="dashSetMobileView('${targetView}')" aria-label="${escHtml(label)}">
      <span aria-hidden="true">←</span>
      <span>${escHtml(label)}</span>
    </button>
  `;
}

window.dashSetMobileView = function(view) {
  setMobileView(view);
};

function normalizeMailboxFolder(folder) {
  return folder === MAILBOX_FOLDER_FAVORITES ? MAILBOX_FOLDER_FAVORITES : MAILBOX_FOLDER_TEMP;
}

function getMailboxFolderMeta(folder = dashState.mailboxFolder) {
  const current = normalizeMailboxFolder(folder);
  if (current === MAILBOX_FOLDER_FAVORITES) {
    return {
      key: MAILBOX_FOLDER_FAVORITES,
      icon: '★',
      title: '收藏夹',
      stripLabel: '收藏邮箱',
      switchLabel: '📬 临时箱',
      switchFolder: MAILBOX_FOLDER_TEMP,
      emptyIcon: '★',
      emptyTitle: '收藏夹还是空的',
      emptyHint: '点邮箱卡片上的 ☆ 之后，会永久保留在这里',
    };
  }
  return {
    key: MAILBOX_FOLDER_TEMP,
    icon: '📬',
    title: '临时邮箱',
    stripLabel: '临时邮箱',
    switchLabel: '★ 收藏夹',
    switchFolder: MAILBOX_FOLDER_FAVORITES,
    emptyIcon: '✉',
    emptyTitle: '还没有临时邮箱',
    emptyHint: '点击「+ 新建」创建第一个',
  };
}

async function loadMailboxPage(targetPage = dashState.mailboxPage) {
  const folder = normalizeMailboxFolder(dashState.mailboxFolder);
  const pageSize = getMailboxPageSize();
  let page = Math.max(1, Number(targetPage) || 1);
  let resp = await api.listMailboxesPage(page, pageSize, folder);
  const total = resp?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  if (page > totalPages) {
    page = totalPages;
    resp = await api.listMailboxesPage(page, pageSize, folder);
  }
  dashState.mailboxFolder = folder;
  dashState.mailboxPage = page;
  applyMailboxPage(resp);
  return resp;
}

async function refreshDashboardMailboxes(targetPage = dashState.mailboxPage) {
  await loadMailboxPage(targetPage);
  paneRenderMailboxes();
  paneRenderEmailView();
  startEmailListPoller();
  await paneRenderEmails();
}

function applyMailboxPage(resp) {
  const rows = resp?.data || [];
  const prevSelectedID = dashState.selectedMailbox?.id || null;
  dashState.mailboxes = rows;
  dashState.mailboxTotal = resp?.total ?? rows.length;
  state.mailboxes = rows;

  const found = prevSelectedID ? rows.find(m => m.id === prevSelectedID) : null;
  dashState.selectedMailbox = found || rows[0] || null;
  if (dashState.selectedMailbox?.id !== prevSelectedID) {
    dashState.selectedEmailId = null;
    dashState.emails = [];
    dashState.emailTotal = 0;
    dashState.emailPage = 1;
  }
  normalizeDashboardMobileView();
}

async function renderDashboard(container) {
  const isAdmin = state.account?.is_admin;
  const publicSettings = await api.publicSettings().catch(() => ({}));
  applyPublicSettings(publicSettings);
  const [_, domains, statsData] = await Promise.all([
    loadMailboxPage(dashState.mailboxPage),
    api.domains(),
    api.stats().catch(() => null),
  ]);

  // 顶栏按钮
  const actions = $('topbar-actions');
  if (actions) {
    actions.innerHTML = `
      <button class="btn btn-primary btn-sm" onclick="createMailbox()">+ 新建邮箱</button>
      <button class="btn btn-ghost btn-sm" onclick="navigate('apikey-show')" style="margin-left:0.4rem">⚿ 我的 API Key</button>
    `;
  }

  const st = statsData || {};
  const activeDomains  = (domains||[]).filter(d => d.is_active).length;
  const pendingDomains = (domains||[]).filter(d => d.status === 'pending').length;

  // 公告栏
  const announcement = publicSettings.announcement || '';

  // 顶部窄统计条（替代原 4-6 张大卡片）
  const folderMeta = getMailboxFolderMeta();
  const stripItems = [
    { label: folderMeta.stripLabel, value: dashState.mailboxTotal },
    { label: '可用域名', value: activeDomains },
    { label: '邮件总数', value: st.total_emails ?? '—' },
    { label: '邮箱总量', value: st.total_mailboxes ?? '—' },
    ...(isAdmin ? [
      { label: '账户数', value: st.total_accounts ?? '—' },
      { label: '待验证', value: st.pending_domains ?? pendingDomains },
    ] : []),
  ];

  container.innerHTML = `
    ${announcement ? `<div class="dashboard-announce">📢 ${escHtml(announcement)}</div>` : ''}
    <div class="dashboard-stats-strip">
      ${stripItems.map(s => `
        <div class="strip-item">
          <span class="strip-value">${typeof s.value === 'number' ? s.value.toLocaleString() : s.value}</span>
          <span class="strip-label">${escHtml(s.label)}</span>
        </div>
      `).join('')}
      ${pendingDomains > 0 ? `<div class="strip-item strip-warn">🔄 ${pendingDomains} 域名验证中</div>` : ''}
    </div>

    <div class="three-pane-grid" data-mobile-view="${dashState.mobileView}">
      <div class="pane pane-mailboxes" id="pane-mailboxes"></div>
      <div class="pane pane-emails" id="pane-emails"></div>
      <div class="pane pane-email-view" id="pane-email-view"></div>
    </div>
  `;

  paneRenderMailboxes();
  paneRenderEmails();
  paneRenderEmailView();
  startEmailListPoller();
}

function paneRenderMailboxes() {
  const pane = $('pane-mailboxes');
  if (!pane) return;
  const folderMeta = getMailboxFolderMeta();
  const total = dashState.mailboxTotal;
  const totalPages = Math.max(1, Math.ceil(total / getMailboxPageSize()));
  if (dashState.mailboxPage > totalPages) dashState.mailboxPage = totalPages;
  const rows = dashState.mailboxes;

  pane.innerHTML = `
    <div class="pane-header">
      <div class="pane-header-main">
        <span class="pane-title">${folderMeta.icon} ${folderMeta.title} (${total})</span>
      </div>
      <div class="pane-header-actions">
        <button class="btn btn-ghost btn-sm" onclick="dashToggleMailboxFolder('${folderMeta.switchFolder}')" title="切换邮箱分组">${folderMeta.switchLabel}</button>
        <button class="btn btn-primary btn-sm" onclick="createMailbox()">+ 新建</button>
      </div>
    </div>
    <div class="pane-scroll">
      ${total === 0
        ? `<div class="empty-state"><span class="empty-icon">${folderMeta.emptyIcon}</span><p>${folderMeta.emptyTitle}</p><p style="font-size:0.78rem;color:var(--text-muted)">${folderMeta.emptyHint}</p></div>`
        : rows.map(mb => buildMailboxCard(mb)).join('')
      }
    </div>
    ${totalPages > 1 ? buildPager('mailbox', dashState.mailboxPage, totalPages) : ''}
  `;
}

function buildMailboxCard(mb) {
  const expiresAt = mb.expires_at ? new Date(mb.expires_at) : null;
  const now = new Date();
  const isFav = !!mb.is_favorite;
  const forwardBadge = buildMailboxForwardBadge(mb);
  let expiryHtml = '';
  if (isFav) {
    expiryHtml = '<span class="mailbox-fav-badge">★ 已收藏</span>';
  } else if (expiresAt) {
    const diffMs = expiresAt - now;
    if (diffMs <= 0) {
      expiryHtml = '<span style="color:var(--clr-danger);font-size:0.75rem">⏱ 已过期</span>';
    } else {
      const mins = Math.ceil(diffMs / 60000);
      const color = mins <= 5 ? 'var(--clr-danger)' : mins <= 15 ? 'var(--clr-warn,#e6a817)' : 'var(--text-muted)';
      expiryHtml = `<span style="color:${color};font-size:0.75rem">⏱ ${mins}分钟后删除</span>`;
    }
  }
  const isSelected = dashState.selectedMailbox?.id === mb.id;
  const favIcon = isFav ? '★' : '☆';
  const favTitle = isFav ? '取消收藏' : '收藏（防过期）';
  return `
    <div class="mailbox-card${isSelected ? ' is-selected' : ''}${isFav ? ' is-favorite' : ''}" onclick="dashSelectMailbox('${mb.id}')">
      <div class="mailbox-address-row">
        <div class="mailbox-address">${escHtml(mb.full_address)}</div>
        ${forwardBadge}
      </div>
      <div class="mailbox-stats" style="display:flex;gap:0.7rem;align-items:center;flex-wrap:wrap">
        <span style="font-size:0.75rem;color:var(--text-muted)">创建于 ${formatDate(mb.created_at)}</span>
        ${expiryHtml}
      </div>
      <div class="mailbox-actions">
        <button class="btn btn-ghost btn-sm" onclick="event.stopPropagation();extractAndCopyCode('${mb.id}','${escHtml(mb.full_address)}')" title="提取最新邮件验证码">🔢 取码</button>
        ${buildMailboxForwardButton(mb, true)}
        <button class="btn btn-ghost btn-sm" onclick="event.stopPropagation();toggleFavorite('${mb.id}',${!isFav})" title="${favTitle}">${favIcon}</button>
        <button class="btn btn-ghost btn-sm" onclick="event.stopPropagation();copyText('${escHtml(mb.full_address)}')" title="复制地址">⎘</button>
        <button class="btn btn-danger btn-sm" onclick="event.stopPropagation();confirmDeleteMailbox('${mb.id}','${escHtml(mb.full_address)}')" title="删除">✕</button>
      </div>
    </div>
  `;
}

window.dashSelectMailbox = function(id) {
  const mb = dashState.mailboxes.find(m => m.id === id);
  if (!mb) return;
  dashState.selectedMailbox = mb;
  dashState.selectedEmailId = null;
  dashState.emails = [];
  dashState.emailTotal = 0;
  dashState.emailPage = 1;
  if (isMobileLayout()) setMobileView('emails');
  else normalizeDashboardMobileView();
  paneRenderMailboxes();
  paneRenderEmails();
  paneRenderEmailView();
  startEmailListPoller();
};

window.dashToggleMailboxFolder = async function(folder) {
  const nextFolder = normalizeMailboxFolder(folder);
  if (nextFolder === dashState.mailboxFolder) return;
  dashState.mailboxFolder = nextFolder;
  dashState.mailboxPage = 1;
  await refreshDashboardMailboxes(1);
};

async function paneRenderEmails() {
  const pane = $('pane-emails');
  if (!pane) return;
  const mb = dashState.selectedMailbox;
  if (!mb) {
    pane.innerHTML = `<div class="pane-header">
      <div class="pane-header-main">
        ${buildPaneBackButton('mailboxes', '返回邮箱')}
        <span class="pane-title">邮件</span>
      </div>
    </div>
      <div class="pane-scroll"><div class="empty-state"><span class="empty-icon">←</span><p>请先在左栏选择一个邮箱</p></div></div>`;
    return;
  }

  const mbIsFav = !!mb.is_favorite;
  pane.innerHTML = `
    <div class="pane-header">
      <div class="pane-header-main">
        ${buildPaneBackButton('mailboxes', '返回邮箱')}
        <span class="pane-title" title="${escHtml(mb.full_address)}">${escHtml(mb.full_address)}</span>
      </div>
      <div class="pane-header-actions">
        ${buildDashboardMailboxHeaderActions(mb)}
      </div>
    </div>
    <div class="pane-scroll" id="pane-emails-scroll"><div style="padding:1rem;text-align:center"><span class="spinner"></span></div></div>
  `;

  let emailsResp;
  try {
    emailsResp = await api.listEmailsPage(mb.id, dashState.emailPage, PAGE_SIZE);
  } catch (e) {
    $('pane-emails-scroll').innerHTML = `<div style="padding:1rem;color:var(--clr-danger)">加载失败：${escHtml(e.message)}</div>`;
    return;
  }
  if (dashState.selectedMailbox?.id !== mb.id) return;
  dashState.emails = emailsResp?.data || [];
  dashState.emailTotal = emailsResp?.total ?? dashState.emails.length;

  const total = dashState.emailTotal;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  if (dashState.emailPage > totalPages) {
    dashState.emailPage = totalPages;
    paneRenderEmails();
    return;
  }
  const rows = dashState.emails;

  const scroll = $('pane-emails-scroll');
  if (!scroll) return;
  if (total === 0) {
    scroll.innerHTML = `<div class="empty-state"><span class="empty-icon">📭</span><p>暂无邮件</p>
      <p style="font-size:0.78rem;color:var(--text-muted)">向 ${escHtml(mb.full_address)} 发送邮件后将出现在这里</p></div>`;
  } else {
    scroll.innerHTML = rows.map(e => buildEmailItem(mb.id, e, dashState.selectedEmailId === e.id)).join('');
  }

  // 翻页栏
  const oldPager = pane.querySelector('.pane-pager'); if (oldPager) oldPager.remove();
  if (totalPages > 1) pane.insertAdjacentHTML('beforeend', buildPager('email', dashState.emailPage, totalPages));
}

window.dashSelectEmail = function(mid, eid) {
  dashState.selectedEmailId = eid;
  if (isMobileLayout()) setMobileView('detail');
  else normalizeDashboardMobileView();
  // 仅刷新右栏 + 中栏的高亮
  paneRenderEmailView();
  // 仅更新中栏列表的选中样式（不重拉数据）
  const scroll = $('pane-emails-scroll');
  if (scroll) {
    scroll.querySelectorAll('.email-item').forEach(node => {
      node.classList.toggle('is-selected', node.dataset.eid === eid);
    });
  }
};

async function paneRenderEmailView() {
  const pane = $('pane-email-view');
  if (!pane) return;
  const mb = dashState.selectedMailbox;
  const eid = dashState.selectedEmailId;
  if (!mb || !eid) {
    pane.innerHTML = `<div class="pane-header">
      <div class="pane-header-main">
        ${buildPaneBackButton(mb ? 'emails' : 'mailboxes', mb ? '返回邮件' : '返回邮箱')}
        <span class="pane-title">邮件正文</span>
      </div>
    </div>
      <div class="pane-scroll"><div class="empty-state"><span class="empty-icon">📨</span><p>请在中栏选择一封邮件</p></div></div>`;
    return;
  }
  pane.innerHTML = `<div class="pane-header">
    <div class="pane-header-main">
      ${buildPaneBackButton('emails', '返回邮件')}
      <span class="pane-title">邮件正文</span>
    </div>
  </div>
    <div class="pane-scroll" id="pane-email-view-scroll"><div style="padding:1rem;text-align:center"><span class="spinner"></span></div></div>`;

  let e;
  try {
    e = await api.getEmail(mb.id, eid);
  } catch (err) {
    $('pane-email-view-scroll').innerHTML = `<div style="padding:1rem;color:var(--clr-danger)">加载失败：${escHtml(err.message)}</div>`;
    return;
  }

  const fromAddr = e.sender || e.from_addr || '—';
  const toAddr   = mb.full_address || '—';
  const htmlBody = e.body_html || e.html_body || '';
  const textBody = e.body_text || e.text_body || '';
  const subject = e.subject || '(无主题)';
  const attachments = Array.isArray(e.attachments) ? e.attachments : [];

  // 顶部 header 改为带操作按钮
  pane.querySelector('.pane-header').innerHTML = `
    <div class="pane-header-main">
      ${buildPaneBackButton('emails', '返回邮件')}
      <span class="pane-title" title="${escHtml(subject)}">${escHtml(subject)}</span>
    </div>
    <div class="pane-header-actions">
      <button class="btn btn-ghost btn-sm" onclick="forwardEmailToTelegram('${mb.id}','${eid}')" title="手动转发该邮件到 Telegram">✈ TG</button>
      <button class="btn btn-ghost btn-sm" onclick="dashExtractCodeFromEmail('${mb.id}','${eid}')" title="从该邮件提取验证码">🔢 取码</button>
      <button class="btn btn-danger btn-sm" onclick="dashDeleteEmail('${mb.id}','${eid}')" title="删除该邮件">✕</button>
    </div>
  `;

  const scroll = $('pane-email-view-scroll');
  scroll.innerHTML = `
    <div class="email-detail-header">
      <div class="email-info-row">
        <span>发件人：<strong>${escHtml(fromAddr)}</strong></span>
        <span style="margin:0 0.3rem">·</span>
        <span>收件人：<strong>${escHtml(toAddr)}</strong></span>
        <span style="margin:0 0.3rem">·</span>
        <span>${formatDate(e.received_at)}</span>
        ${attachments.length ? `<span style="margin:0 0.3rem">·</span><span>附件 ${attachments.length}</span>` : ''}
      </div>
    </div>
    ${buildEmailAttachments(mb.id, eid, attachments)}
    ${htmlBody
      ? `<iframe class="email-body-frame" id="email-frame-pane" sandbox="allow-same-origin allow-popups"></iframe>`
      : `<div class="email-body-text" style="white-space:pre-wrap;padding:0.8rem 1rem">${escHtml(textBody || '(邮件内容为空)')}</div>`
    }
  `;

  if (htmlBody) {
    const frame = scroll.querySelector('#email-frame-pane');
    if (frame) {
      frame.contentDocument.open();
      frame.contentDocument.write(htmlBody);
      frame.contentDocument.close();
      const setH = () => {
        try { frame.style.height = frame.contentDocument.body.scrollHeight + 20 + 'px'; } catch (_) {}
      };
      frame.addEventListener('load', setH);
      setTimeout(setH, 300);
    }
  }
}

window.dashDeleteEmail = async function(mid, eid) {
  try {
    await api.deleteEmail(mid, eid);
    toast('邮件已删除', 'success');
    if (dashState.selectedEmailId === eid) {
      dashState.selectedEmailId = null;
      if (isMobileLayout()) setMobileView('emails');
    }
    normalizeDashboardMobileView();
    paneRenderEmails();
    paneRenderEmailView();
  } catch (err) { toast('删除失败：' + err.message, 'error'); }
};

window.downloadEmailAttachment = async function(mid, eid, attachmentId) {
  try {
    const result = await api.downloadEmailAttachment(mid, eid, attachmentId);
    triggerBlobDownload(result.blob, result.filename || `attachment-${attachmentId}`);
  } catch (err) {
    toast('附件下载失败：' + err.message, 'error');
  }
};

window.forwardEmailToTelegram = async function(mid, eid) {
  try {
    const res = await api.forwardEmailToTG(mid, eid);
    const count = Number(res.attachments_sent) || 0;
    toast(count > 0 ? `已转发到 TG（含 ${count} 个附件）` : '已转发到 TG', 'success');
  } catch (err) {
    toast('转发到 TG 失败：' + err.message, 'error');
  }
};

window.dashSetPage = async function(kind, targetPage) {
  if (kind === 'mailbox') {
    const totalPages = Math.max(1, Math.ceil((dashState.mailboxTotal || 0) / getMailboxPageSize()));
    dashState.mailboxPage = Math.min(totalPages, Math.max(1, Number(targetPage) || 1));
    await refreshDashboardMailboxes(dashState.mailboxPage);
    return;
  }

  if (kind === 'email') {
    const totalPages = Math.max(1, Math.ceil((dashState.emailTotal || 0) / PAGE_SIZE));
    dashState.emailPage = Math.min(totalPages, Math.max(1, Number(targetPage) || 1));
    await paneRenderEmails();
  }
};

window.dashChangePage = async function(kind, delta) {
  if (kind === 'mailbox') {
    await window.dashSetPage(kind, dashState.mailboxPage + delta);
  } else if (kind === 'email') {
    await window.dashSetPage(kind, dashState.emailPage + delta);
  }
};

window.dashPromptPage = async function(kind) {
  const current = kind === 'mailbox' ? dashState.mailboxPage : dashState.emailPage;
  const totalPages = kind === 'mailbox'
    ? Math.max(1, Math.ceil((dashState.mailboxTotal || 0) / getMailboxPageSize()))
    : Math.max(1, Math.ceil((dashState.emailTotal || 0) / PAGE_SIZE));
  const raw = window.prompt(`输入要跳转的页码（1-${totalPages}）`, String(current));
  if (raw === null) return;
  const page = Math.floor(Number(raw));
  if (!Number.isFinite(page) || page < 1 || page > totalPages) {
    toast(`请输入 1 到 ${totalPages} 之间的页码`, 'warn');
    return;
  }
  await window.dashSetPage(kind, page);
};

function buildPager(kind, page, totalPages) {
  const isFirst = page <= 1;
  const isLast = page >= totalPages;
  return `
    <div class="pane-pager">
      <button class="btn btn-ghost btn-sm pager-btn" onclick="dashSetPage('${kind}',1)" ${isFirst ? 'disabled' : ''} aria-label="首页">《</button>
      <button class="btn btn-ghost btn-sm pager-btn" onclick="dashSetPage('${kind}',${page - 1})" ${isFirst ? 'disabled' : ''} aria-label="上一页">&lt;</button>
      <button class="pane-pager-status pane-pager-jump" type="button" onclick="dashPromptPage('${kind}')" title="点击输入页码直达">${page} / ${totalPages}</button>
      <button class="btn btn-ghost btn-sm pager-btn" onclick="dashSetPage('${kind}',${page + 1})" ${isLast ? 'disabled' : ''} aria-label="下一页">&gt;</button>
      <button class="btn btn-ghost btn-sm pager-btn" onclick="dashSetPage('${kind}',${totalPages})" ${isLast ? 'disabled' : ''} aria-label="末页">》</button>
    </div>
  `;
}

// 邮件列表轮询：选中邮箱时每 8s 刷新一次
function startEmailListPoller() {
  stopEmailListPoller();
  if (!dashState.selectedMailbox) return;
  dashState.emailListPoller = setInterval(async () => {
    if (state.page !== 'dashboard' || !dashState.selectedMailbox) {
      stopEmailListPoller();
      return;
    }
    try {
      const fresh = await api.listEmailsPage(dashState.selectedMailbox.id, dashState.emailPage, PAGE_SIZE);
      const freshRows = fresh?.data || [];
      const oldRows = dashState.emails || [];
      const totalChanged = (fresh?.total ?? freshRows.length) !== dashState.emailTotal;
      const firstChanged = freshRows[0]?.id !== oldRows[0]?.id;
      if (totalChanged || firstChanged) {
        dashState.emails = freshRows;
        dashState.emailTotal = fresh?.total ?? freshRows.length;
        if (dashState.selectedEmailId && !freshRows.some(e => e.id === dashState.selectedEmailId)) {
          dashState.selectedEmailId = null;
          normalizeDashboardMobileView();
          paneRenderEmailView();
        }
        const scroll = $('pane-emails-scroll');
        const pane = $('pane-emails');
        if (scroll) {
          scroll.innerHTML = freshRows.map(e => buildEmailItem(dashState.selectedMailbox.id, e, dashState.selectedEmailId === e.id)).join('') ||
            `<div class="empty-state"><span class="empty-icon">📭</span><p>暂无邮件</p></div>`;
        }
        if (pane) {
          const totalPages = Math.max(1, Math.ceil(dashState.emailTotal / PAGE_SIZE));
          const oldPager = pane.querySelector('.pane-pager');
          if (oldPager) oldPager.remove();
          if (totalPages > 1) pane.insertAdjacentHTML('beforeend', buildPager('email', dashState.emailPage, totalPages));
        }
      }
    } catch (_) { /* 静默 */ }
  }, 8000);
}

function stopEmailListPoller() {
  if (dashState.emailListPoller) {
    clearInterval(dashState.emailListPoller);
    dashState.emailListPoller = null;
  }
}

let _dashboardResizeTimer = null;
window.addEventListener('resize', () => {
  clearTimeout(_dashboardResizeTimer);
  _dashboardResizeTimer = setTimeout(() => {
    if (!isMobileLayout() && dashState.mobileView !== 'mailboxes') {
      setMobileView('mailboxes');
    }
    if (!window.matchMedia('(max-width: 1024px)').matches) {
      closeSidebar();
    }
    if (state.page === 'dashboard') {
      normalizeDashboardMobileView();
    }
  }, 120);
});

// ─── 收藏 / 取消收藏 ─────────────────────────────────────────
window.toggleFavorite = async function(id, fav) {
  try {
    const res = await apiFetch(API_BASE + '/mailboxes/' + id + '/favorite', {
      method: 'PUT',
      body: JSON.stringify({ favorite: !!fav }),
    });
    const updated = res.mailbox || res;
    syncMailboxState(updated);
    if (state.page === 'dashboard') {
      await refreshDashboardMailboxes(dashState.mailboxPage);
    }
    if (state.page === 'inbox') renderInboxTopbarActions(updated);
    toast(fav ? '已收藏（不会被自动清理）' : '已取消收藏', 'success');
  } catch (e) {
    toast('操作失败：' + e.message, 'error');
  }
};

window.toggleMailboxForward = async function(id, enabled) {
  try {
    const updated = await api.setMailboxForward(id, enabled);
    syncMailboxState(updated);
    paneRenderMailboxes();
    if (state.page === 'dashboard') paneRenderEmails();
    if (state.page === 'inbox') renderInboxTopbarActions(updated);
    toast(enabled ? 'TG 转发已开启' : 'TG 转发已关闭', 'success');
  } catch (e) {
    toast('TG 转发设置失败：' + e.message, 'error');
  }
};

// ─── 验证码提取 ─────────────────────────────────────────────
function extractCode(text) {
  if (!text) return null;
  const t = String(text).replace(/\s+/g, ' ');
  let m = t.match(/(?:验证码|校验码|动态码|安全码|verif(?:y|ication)?\s*code|one[\- ]?time\s*(?:code|password)|otp|pin|security\s*code)[^A-Za-z0-9]{0,8}([A-Za-z0-9]{4,8})/i);
  if (m) return m[1].toUpperCase();
  m = t.match(/(?:验证码|校验码|动态码|安全码|verif(?:y|ication)?\s*code|one[\- ]?time\s*(?:code|password)|otp|pin|security\s*code).{0,40}?(\d{4,8})(?:\D|$)/i);
  if (m) return m[1];
  m = t.match(/(?:验证码|校验码|动态码|安全码|verif(?:y|ication)?\s*code|one[\- ]?time\s*(?:code|password)|otp|pin|security\s*code)[^0-9]{0,16}((?:\d[\s\-]){3,7}\d)/i);
  if (m) return m[1].replace(/[\s\-]+/g, '');
  m = t.match(/(^|[^A-Za-z0-9])(\d{4,8})(?:$|[^A-Za-z0-9])/);
  if (m) return m[2];
  m = t.match(/(^|[^0-9])((?:\d[\s\-]){3,7}\d)(?:$|[^0-9])/);
  if (m) return m[2].replace(/[\s\-]+/g, '');
  m = t.match(/(^|[^A-Za-z0-9])([A-Z0-9]{4,8})(?:$|[^A-Za-z0-9])/);
  if (m && /[A-Z]/.test(m[2]) && /\d/.test(m[2])) return m[2];
  return null;
}

function extractCodeFromHtml(html) {
  if (!html) return null;
  const cleaned = String(html)
    .replace(/<!--.*?-->/gs, ' ')
    .replace(/<(?:style|script)[^>]*>.*?<\/(?:style|script)\s*>/gis, ' ');
  const matches = cleaned.matchAll(/>\s*([A-Za-z0-9][A-Za-z0-9\s\-]{2,14}[A-Za-z0-9])\s*</g);
  for (const m of matches) {
    const code = String(m[1] || '').replace(/[^A-Za-z0-9]/g, '').toUpperCase();
    if (code.length >= 4 && code.length <= 8 && /\d/.test(code)) return code;
  }
  return null;
}

function stripHtml(html) {
  if (!html) return '';
  const tmp = document.createElement('div');
  tmp.innerHTML = String(html)
    .replace(/<!--.*?-->/gs, ' ')
    .replace(/<(?:style|script)[^>]*>.*?<\/(?:style|script)\s*>/gis, ' ');
  return tmp.textContent || tmp.innerText || '';
}

window.extractAndCopyCode = async function(mailboxID, fullAddress) {
  try {
    const res = await api.latestOTP(mailboxID);
    if (!res || !res.code) {
      toast('未识别到验证码', 'warn');
      return;
    }
    await navigator.clipboard.writeText(res.code).catch(() => {});
    toast(`已复制验证码：${res.code}`, 'success');
  } catch (e) {
    if (String(e.message || '').includes('no emails found')) {
      toast('该邮箱暂无邮件', 'warn');
      return;
    }
    if (String(e.message || '').includes('otp not found')) {
      toast('未识别到验证码', 'warn');
      return;
    }
    toast('提取失败：' + e.message, 'error');
  }
};

window.dashExtractCodeFromEmail = async function(mailboxID, emailID) {
  try {
    const full = await api.getEmail(mailboxID, emailID);
    const code = extractCodeFromHtml(full.body_html || '') || extractCode((full.body_text || '') + '\n' + stripHtml(full.body_html || '') + '\n' + (full.subject || ''));
    
    if (!code) { toast('未识别到验证码', 'warn'); return; }
    await navigator.clipboard.writeText(code).catch(() => {});
    toast(`已复制验证码：${code}`, 'success');
  } catch (e) {
    toast('提取失败：' + e.message, 'error');
  }
};

window.openInbox = function(id, addr) {
  state.currentMailbox = state.mailboxes.find(mb => mb.id === id) || { id, full_address: addr };
  navigate('inbox');
};

window.createMailbox = async function() {
  // 拉取活跃域名列表，构建选择弹窗
  let activeDomains = [];
  try {
    const all = await api.domains();
    activeDomains = (all || []).filter(d => d.is_active);
  } catch(e) { /* 获取失败时退化为随机域名 */ }

  const old = document.querySelector('.modal-overlay');
  if (old) old.remove();
  const overlay = el('div', 'modal-overlay');

  const domainOptions = activeDomains.map(d =>
    `<option value="${escHtml(d.domain)}" data-sub-enabled="${d.subdomain_enabled ? '1' : '0'}" data-sub-len="${Number(d.subdomain_random_length || 5)}">${escHtml(d.domain)}${d.subdomain_enabled ? ' ✦' : ''}</option>`
  ).join('');
  const anySubdomainDomain = activeDomains.some(d => d.subdomain_enabled);

  overlay.innerHTML = `
    <div class="modal" style="max-width:460px">
      <div class="modal-title">+ 新建临时邮箱</div>
      <button class="modal-close" onclick="this.closest('.modal-overlay').remove()">✕</button>
      <div class="form-group" style="margin-top:0.8rem">
        <label class="form-label">本地部分（@ 之前）</label>
        <input class="form-input" id="mb-address" placeholder="留空则随机生成" autocomplete="off" />
        <div class="form-hint">只允许字母、数字、连字符、下划线</div>
      </div>
      <div class="form-group">
        <label class="form-label">域名</label>
        <select class="form-input" id="mb-domain">
          <option value="" data-sub-enabled="${anySubdomainDomain ? '1' : '0'}" data-sub-len="5">随机选取</option>
          ${domainOptions}
        </select>
        <div class="form-hint">${anySubdomainDomain ? '带 ✦ 的域名支持多级子域名' : ''}</div>
      </div>

      <div id="mb-sub-section" style="display:none;border:1px dashed var(--border-color,#d1d5db);border-radius:6px;padding:0.7rem 0.85rem;margin-bottom:0.8rem">
        <div class="toggle-wrap" style="margin-bottom:0.4rem">
          <label class="toggle">
            <input type="checkbox" id="mb-sub-toggle">
            <span class="toggle-slider"></span>
          </label>
          <div>
            <div class="toggle-label">使用多级子域名</div>
            <span class="toggle-desc" style="font-size:0.74rem">生成形如 <code>local@xxx.domain</code> 的地址</span>
          </div>
        </div>
        <div id="mb-sub-detail" style="display:none">
          <div class="form-group" style="margin-bottom:0.4rem">
            <label class="form-label" style="font-size:0.78rem">子域模式</label>
            <select class="form-input" id="mb-sub-mode">
              <option value="random">随机生成</option>
              <option value="custom">自定义</option>
            </select>
          </div>
          <div class="form-group" id="mb-sub-len-wrap" style="margin-bottom:0.4rem">
            <label class="form-label" style="font-size:0.78rem">随机长度（2–8）</label>
            <input class="form-input" id="mb-sub-length" type="number" min="2" max="8" value="5" />
          </div>
          <div class="form-group" id="mb-sub-custom-wrap" style="display:none;margin-bottom:0.2rem">
            <label class="form-label" style="font-size:0.78rem">子域字符串</label>
            <input class="form-input" id="mb-sub-value" placeholder="abc12" maxlength="8" />
            <div class="form-hint">仅允许 2–8 位 a–z 0–9</div>
          </div>
        </div>
      </div>

      <div class="modal-actions">
        <button class="btn btn-ghost" onclick="this.closest('.modal-overlay').remove()">取消</button>
        <button class="btn btn-primary" id="mb-confirm-btn">创建</button>
      </div>
    </div>
  `;
  document.body.appendChild(overlay);
  overlay.addEventListener('click', e => { if (e.target === overlay) overlay.remove(); });

  const domainSel = overlay.querySelector('#mb-domain');
  const subSection = overlay.querySelector('#mb-sub-section');
  const subToggle = overlay.querySelector('#mb-sub-toggle');
  const subDetail = overlay.querySelector('#mb-sub-detail');
  const subMode = overlay.querySelector('#mb-sub-mode');
  const subLenWrap = overlay.querySelector('#mb-sub-len-wrap');
  const subLen = overlay.querySelector('#mb-sub-length');
  const subCustomWrap = overlay.querySelector('#mb-sub-custom-wrap');

  function refreshSubSection() {
    const opt = domainSel.options[domainSel.selectedIndex];
    const enabled = opt && opt.dataset.subEnabled === '1';
    const defaultLen = Number(opt?.dataset.subLen || 5);
    if (enabled) {
      subSection.style.display = '';
      subLen.value = String(Math.max(2, Math.min(8, defaultLen)));
    } else {
      subSection.style.display = 'none';
      subToggle.checked = false;
      subDetail.style.display = 'none';
    }
  }
  function refreshSubDetail() {
    subDetail.style.display = subToggle.checked ? '' : 'none';
    if (!subToggle.checked) return;
    if (subMode.value === 'custom') {
      subLenWrap.style.display = 'none';
      subCustomWrap.style.display = '';
    } else {
      subLenWrap.style.display = '';
      subCustomWrap.style.display = 'none';
    }
  }
  domainSel.addEventListener('change', refreshSubSection);
  subToggle.addEventListener('change', refreshSubDetail);
  subMode.addEventListener('change', refreshSubDetail);
  refreshSubSection();
  refreshSubDetail();

  // 回车确认
  overlay.querySelector('#mb-address').addEventListener('keydown', e => {
    if (e.key === 'Enter') overlay.querySelector('#mb-confirm-btn').click();
  });

  overlay.querySelector('#mb-confirm-btn').addEventListener('click', async () => {
    const btn     = overlay.querySelector('#mb-confirm-btn');
    const address = overlay.querySelector('#mb-address').value.trim();
    const domain  = domainSel.value;
    btn.disabled  = true;
    btn.textContent = '创建中...';
    try {
      const body = { source: 'web' };
      if (address) body.address = address;
      if (domain)  body.domain  = domain;
      if (subSection.style.display !== 'none' && subToggle.checked) {
        const mode = subMode.value;
        body.subdomain_mode = mode;
        if (mode === 'custom') {
          const sub = (overlay.querySelector('#mb-sub-value')?.value || '').trim().toLowerCase();
          if (!/^[a-z0-9]{2,8}$/.test(sub)) {
            btn.disabled = false; btn.textContent = '创建';
            toast('子域格式无效（仅 2–8 位 a–z 0–9）', 'warn');
            return;
          }
          body.subdomain = sub;
        } else {
          const n = Number(subLen.value);
          if (Number.isFinite(n) && n >= 2 && n <= 8) body.subdomain_length = n;
        }
      } else if (subSection.style.display !== 'none') {
        body.subdomain_mode = 'off';
      }
      const resp = await apiFetch(API_BASE + '/mailboxes', { method: 'POST', body: JSON.stringify(body) });
      const mb = resp.mailbox || resp;
      overlay.remove();
      const tag = resp.format_version === 'v2' ? ' (v2)' : '';
      toast(`已创建：${mb.full_address}${tag}`, 'success');
      if (resp.fallback_reason) {
        toast('已回退至 v1：该域名未启用多级子域', 'warn');
      }
      dashState.mailboxFolder = MAILBOX_FOLDER_TEMP;
      dashState.mailboxPage = 1;
      navigate('dashboard');
    } catch(e) {
      btn.disabled = false;
      btn.textContent = '创建';
      toast('创建失败：' + e.message, 'error');
    }
  });
};

window.confirmDeleteMailbox = function(id, addr) {
  showModal(`删除邮箱`, `<p>确定删除 <strong>${escHtml(addr)}</strong>？<br/><span style="font-size:0.8rem;color:var(--clr-danger)">所有邮件将被永久删除。</span></p>`,
    async () => {
      try {
        await api.deleteMailbox(id);
        toast('邮箱已删除', 'success');
        // 删除当前选中邮箱时清掉选中状态，避免后续 paneRenderEmails 拉一个不存在的邮箱
        if (dashState.selectedMailbox?.id === id) {
          dashState.selectedMailbox = null;
          dashState.selectedEmailId = null;
          dashState.emails = [];
          dashState.emailTotal = 0;
          dashState.emailPage = 1;
        }
        if (state.currentMailbox?.id === id) state.currentMailbox = null;
        navigate('dashboard');
      } catch(e) { toast('删除失败: ' + e.message, 'error'); }
    }
  );
};

// ─── API Key 展示 ──────────────────────────────────────────
function renderApiKeyShow(container) {
  const key = state.apiKey || '—';
  container.innerHTML = `
    <div class="card" style="max-width:540px">
      <div class="card-header"><div class="card-title">⚿ 我的 API Key</div></div>
      <div class="card-body">
        <p style="font-size:0.84rem;color:var(--text-secondary);margin-bottom:1rem">
          API Key 用于认证所有 API 请求。请勿泄露。
        </p>
        <div class="form-label">当前 API Key</div>
        <div class="code-box" style="margin-bottom:1rem">
          <span style="filter:blur(4px);cursor:pointer" id="key-blur" onclick="this.style.filter='none'">${escHtml(key)}</span>
          <button class="copy-btn" onclick="copyText('${escHtml(key)}')" title="复制">⎘</button>
        </div>
        <p style="font-size:0.76rem;color:var(--text-muted)">点击 Key 可显示明文。保存后请妥善保管，丢失需联系管理员重置。</p>
        <div class="divider"></div>
        <div class="form-label">HTTP 请求示例</div>
        <div class="code-box" style="font-size:0.75rem">curl -H "Authorization: Bearer &lt;api_key&gt;" http://server:8080/api/mailboxes</div>
      </div>
    </div>
  `;
}

// ─── Inbox ────────────────────────────────────────────────
async function renderInbox(container) {
  const mb = state.currentMailbox;
  if (!mb) { navigate('dashboard'); return; }

  const title = $('topbar-title'); if (title) title.textContent = mb.full_address;
  const sub   = $('topbar-subtitle'); if (sub) sub.textContent = '邮件列表';
  renderInboxTopbarActions(mb);

  const emails = await api.listEmails(mb.id);
  state.emails = emails || [];

  // 启动自动刷新（每 8 秒）
  clearInboxPoller();
  _inboxPollerTimer = setInterval(async () => {
    if (state.page !== 'inbox') { clearInboxPoller(); return; }
    try {
      const fresh = await api.listEmails(mb.id);
      if (!fresh) return;
      // 有新邮件才重新渲染，避免闪烁
      if (fresh.length !== (state.emails || []).length ||
          (fresh[0]?.id !== state.emails?.[0]?.id)) {
        state.emails = fresh;
        const c = $('page-content');
        if (c) renderInbox(c);
      }
    } catch(e) { /* 静默失败 */ }
  }, 8000);

  if (!state.emails.length) {
    container.innerHTML = `
      <div class="card">
        <div class="empty-state">
          <span class="empty-icon">📭</span>
          <p>暂无邮件</p>
          <p style="margin-top:0.5rem;font-size:0.8rem">向 <strong>${escHtml(mb.full_address)}</strong> 发送邮件后，邮件将显示在此处</p>
        </div>
      </div>
    `;
    return;
  }

  container.innerHTML = `
    <div class="card" style="padding:0">
      ${state.emails.map(e => buildEmailItem(mb.id, e)).join('')}
    </div>
  `;
}

function buildEmailItem(mbId, e, isSelected, mode) {
  const from = e.sender || e.from_addr || '(无发件人)';
  const initials = from.charAt(0).toUpperCase();
  const preview = (e.body_text || e.text_body || '').slice(0, 80).replace(/\n/g, ' ');
  // mode='dashboard'（默认在三栏内点击只刷新右栏）；其它情况走旧的全页跳转
  const inDash = (mode === 'dashboard') || (state.page === 'dashboard');
  const onClick = inDash
    ? `dashSelectEmail('${mbId}','${e.id}')`
    : `openEmail('${mbId}','${e.id}')`;
  const onDelete = inDash
    ? `dashDeleteEmail('${mbId}','${e.id}')`
    : `deleteEmail('${mbId}','${e.id}')`;
  return `
    <div class="email-item${isSelected ? ' is-selected' : ''}" data-eid="${e.id}" onclick="${onClick}">
      <div class="email-avatar">${escHtml(initials)}</div>
      <div class="email-meta">
        <div class="email-from">${escHtml(from)}</div>
        <div class="email-subject">${escHtml(e.subject || '(无主题)')}</div>
        <div class="email-preview">${escHtml(preview)}</div>
      </div>
      <div>
        <div class="email-time">${timeAgo(e.received_at)}</div>
        <button class="btn btn-ghost btn-sm" style="margin-top:0.3rem" onclick="event.stopPropagation();${onDelete}">✕</button>
      </div>
    </div>
  `;
}

window.openEmail = function(mbId, eid) {
  state.currentMailbox = state.currentMailbox || { id: mbId };
  state.currentEmailId = eid;
  navigate('email-view');
};

window.refreshInbox = function() {
  clearInboxPoller();
  renderPage('inbox');
};

window.deleteEmail = async function(mbId, eid) {
  try {
    await api.deleteEmail(mbId, eid);
    toast('邮件已删除', 'success');
    navigate('inbox');
  } catch(e) { toast('删除失败: ' + e.message, 'error'); }
};

// ─── Email View ────────────────────────────────────────────
async function renderEmailView(container) {
  const mb = state.currentMailbox;
  const eid = state.currentEmailId;
  if (!mb || !eid) { navigate('dashboard'); return; }

  const actions = $('topbar-actions');
  if (actions) {
    actions.innerHTML = `
      <button class="btn btn-ghost btn-sm" onclick="navigate('inbox')">← 返回列表</button>
      <button class="btn btn-ghost btn-sm" onclick="forwardEmailToTelegram('${mb.id}','${eid}')" style="margin-left:0.4rem">✈ TG 转发</button>
      <button class="btn btn-danger btn-sm" onclick="deleteEmail('${mb.id}','${eid}');navigate('inbox')" style="margin-left:0.4rem">删除</button>
    `;
  }

  const e = await api.getEmail(mb.id, eid);
  const fromAddr = e.sender || e.from_addr || '—';
  const toAddr   = mb.full_address || state.currentMailbox?.full_address || '—';
  const htmlBody  = e.body_html || e.html_body || '';
  const textBody  = e.body_text || e.text_body || '';
  const attachments = Array.isArray(e.attachments) ? e.attachments : [];
  const title = $('topbar-title'); if (title) title.textContent = e.subject || '(无主题)';
  const sub   = $('topbar-subtitle'); if (sub) sub.textContent = `来自：${fromAddr}`;

  // 先渲染完整 HTML（含 iframe 占位），再向 iframe 写入内容
  container.innerHTML = `
    <div class="card" style="padding:0;max-width:860px">
      <div class="email-detail-header">
        <div class="email-subject-big">${escHtml(e.subject || '(无主题)')}</div>
        <div class="email-info-row">
          <span>发件人：<strong>${escHtml(fromAddr)}</strong></span>
          <span style="margin:0 0.3rem">·</span>
          <span>收件人：<strong>${escHtml(toAddr)}</strong></span>
          <span style="margin:0 0.3rem">·</span>
          <span>${formatDate(e.received_at)}</span>
          ${attachments.length ? `<span style="margin:0 0.3rem">·</span><span>附件 ${attachments.length}</span>` : ''}
        </div>
      </div>
      ${buildEmailAttachments(mb.id, eid, attachments)}
      ${htmlBody
        ? `<iframe class="email-body-frame" id="email-frame" sandbox="allow-same-origin allow-popups"></iframe>`
        : `<div class="email-body-text" style="white-space:pre-wrap">${escHtml(textBody || '(邮件内容为空)')}</div>`
      }
    </div>
  `;

  // innerHTML 中的 <script> 不会执行；在 DOM 就绪后直接向 iframe 写内容
  if (htmlBody) {
    const frame = container.querySelector('#email-frame');
    if (frame) {
      frame.contentDocument.open();
      frame.contentDocument.write(htmlBody);
      frame.contentDocument.close();
      const setH = () => {
        try { frame.style.height = frame.contentDocument.body.scrollHeight + 20 + 'px'; } catch (_) {}
      };
      frame.addEventListener('load', setH);
      setTimeout(setH, 300);
    }
  }
}

// ─── 域名列表 & 指南 ─────────────────────────────────────────
async function renderDomainsGuide(container) {
  const actions = $('topbar-actions');
  if (actions) {
    actions.innerHTML = `<button class="btn btn-success btn-sm" onclick="showMXRegisterModal()">⚡ 提交域名自动验证</button>`;
  }

  const [domains, pub] = await Promise.all([
    api.domains(),
    api.publicSettings().catch(() => ({})),
  ]);
  const smtpIP  = pub.smtp_server_ip || '';
  const smtpHostname = pub.smtp_hostname || '';
  const ipLabel = smtpIP || '&lt;服务器 IP&gt;';
  const mxTarget = smtpHostname || '&lt;服务器邮件主机名&gt;';
  const needsARec = !smtpHostname;

  const pending = (domains||[]).filter(d => d.status === 'pending');
  const active  = (domains||[]).filter(d => d.status !== 'pending');

  const pendingHtml = pending.length > 0 ? `
    <div class="card" style="border-left:3px solid var(--clr-warn,#e6a817);margin-bottom:1rem">
      <div class="card-header">
        <div class="card-title">🔄 待 MX 验证 (${pending.length})</div>
        <div style="font-size:0.78rem;color:var(--text-muted)">后台每 30 秒自动检测，验证通过后自动激活</div>
      </div>
      <div class="table-wrap">
        <table class="stack-table">
          <thead><tr><th>域名</th><th>上次检测</th><th>状态</th></tr></thead>
          <tbody>
            ${pending.map(d => `
              <tr id="pending-row-${d.id}">
                ${buildDataLabel('域名', `<span style="font-family:var(--font-mono);font-size:0.82rem">${escHtml(d.domain)}</span>`)}
                ${buildDataLabel('上次检测', d.mx_checked_at ? timeAgo(d.mx_checked_at) : '待首次检测', 'style="font-size:0.78rem"')}
                ${buildDataLabel('状态', `<span class="badge badge-gold" id="pending-status-${d.id}">⏳ 检测中</span>`)}
              </tr>
            `).join('')}
          </tbody>
        </table>
      </div>
    </div>
  ` : '';

  container.innerHTML = `
    ${pendingHtml}
    <div class="domain-guide-grid" style="display:grid;grid-template-columns:1fr 1fr;gap:1.2rem;max-width:1000px">
      <div>
        <div class="card">
          <div class="card-header"><div class="card-title">◎ 可用域名池</div></div>
          <div class="table-wrap">
            <table class="stack-table">
              <thead><tr><th>域名</th><th>状态</th></tr></thead>
              <tbody>
                ${active.length === 0
                  ? `<tr><td colspan="2" style="text-align:center;color:var(--text-muted)">暂无域名</td></tr>`
                  : active.map(d => `
                    <tr>
                      ${buildDataLabel('域名', `<span style="font-family:var(--font-mono);font-size:0.82rem">${escHtml(d.domain)}</span>`)}
                      ${buildDataLabel('状态', d.is_active
                        ? '<span class="badge badge-green">● 启用</span>'
                        : '<span class="badge badge-gray">○ 停用</span>')}
                    </tr>
                  `).join('')}
              </tbody>
            </table>
          </div>
        </div>
      </div>

      <div>
        <div class="card">
          <div class="card-header"><div class="card-title">📖 添加域名指南</div></div>
          <div class="card-body">
            <div class="guide-step">
              <div class="step-num">1</div>
              <div class="step-body">
                <div class="step-title">准备域名</div>
                <div class="step-desc">在域名注册商处购买一个域名，例如 <code>example.com</code>，并获取 DNS 管理权限。</div>
              </div>
            </div>
            <div class="guide-step">
              <div class="step-num">2</div>
              <div class="step-body">
                <div class="step-title">配置 MX 记录（仅需一条）</div>
                <div class="step-desc">在 DNS 面板添加以下记录，让 SMTP 邮件投递到本服务器：</div>
                <table class="dns-table stack-table" style="margin-top:0.5rem">
                  <thead><tr><th>类型</th><th>主机名</th><th>内容</th><th>优先级</th></tr></thead>
                  <tbody>
                    <tr>
                      ${buildDataLabel('类型', 'MX')}
                      ${buildDataLabel('主机名', '@')}
                      ${buildDataLabel('内容', `<span style="font-family:monospace">${mxTarget}</span>`)}
                      ${buildDataLabel('优先级', '10')}
                    </tr>
                    ${needsARec ? `<tr>
                      ${buildDataLabel('类型', 'A')}
                      ${buildDataLabel('主机名', '<span style="font-family:monospace">mail.yourdomain.com</span>')}
                      ${buildDataLabel('内容', `<span style="font-family:monospace">${ipLabel}</span>`)}
                      ${buildDataLabel('优先级', '—')}
                    </tr>` : ''}
                    <tr>
                      ${buildDataLabel('类型', 'TXT')}
                      ${buildDataLabel('主机名', '@')}
                      ${buildDataLabel('内容', `<span style="font-family:monospace">v=spf1 ip4:${ipLabel} ~all</span>`)}
                      ${buildDataLabel('优先级', '—')}
                    </tr>
                    <tr style="background:rgba(34,197,94,.06)">
                      ${buildDataLabel('类型', 'MX')}
                      ${buildDataLabel('主机名', '<span style="font-family:monospace">*</span>')}
                      ${buildDataLabel('内容', `<span style="font-family:monospace">${mxTarget}</span>`)}
                      ${buildDataLabel('优先级', '10')}
                    </tr>
                  </tbody>
                </table>
                <div style="font-size:0.78rem;color:var(--text-muted);margin-top:0.4rem">
                  ✦ 末行为<strong>通配 MX</strong>：仅在希望启用「多级子域名」（如 <code>xxx@aa.bb.cc.dd</code>）时需要；纯顶级邮箱可省略。
                </div>
              </div>
            </div>
            <div class="guide-step">
              <div class="step-num">3</div>
              <div class="step-body">
                <div class="step-title">提交域名自动验证</div>
                <div class="step-desc">
                  DNS 广播后（通常 5–30 分钟），点击右上角「⚡ 提交域名自动验证」按钮。<br>
                  <ul style="margin:0.4rem 0 0 1rem;font-size:0.82rem">
                    <li>MX 已生效 → <b>立即激活</b>加入域名池</li>
                    <li>MX 未生效 → 进入<b>待验证队列</b>，后台每 30 秒自动重试</li>
                  </ul>
                </div>
                <button class="btn btn-success btn-sm" style="margin-top:0.5rem" onclick="showMXRegisterModal()">⚡ 提交域名</button>
              </div>
            </div>
            <div class="guide-step">
              <div class="step-num">4</div>
              <div class="step-body">
                <div class="step-title">验证收信</div>
                <div class="step-desc">域名激活后，创建该域名下的邮箱，用其他邮件客户端发送测试邮件，30 秒内应能收到。</div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  `;

  if (pending.length > 0) {
    startPendingDomainPoller(pending.map(d => d.id));
  }
}

// ─── Admin: 账户管理 ─────────────────────────────────────────
function mergeAdminOTPShareMailboxes(favorites, all) {
  const merged = [];
  const seen = new Set();
  [...(favorites || []), ...(all || [])].forEach(mailbox => {
    if (!mailbox || !mailbox.id || seen.has(mailbox.id)) return;
    seen.add(mailbox.id);
    merged.push(mailbox);
  });
  return merged;
}

function buildAdminOTPShareOptions(mailboxes) {
  return (mailboxes || []).map(mailbox => {
    const label = mailbox.is_favorite ? `${mailbox.full_address} ★` : mailbox.full_address;
    return `<option value="${escHtml(mailbox.full_address)}" label="${escHtml(label)}"></option>`;
  }).join('');
}

function buildAdminOTPShareResult() {
  const current = state.adminOTPShare || {};
  const mailbox = current.mailbox;
  const share = current.share;
  if (!mailbox) {
    return `
      <div class="otp-share-empty">
        输入一个属于当前账号的邮箱地址，或从收藏邮箱建议中选择，然后读取或生成分享。
      </div>
    `;
  }

  const tokenBox = share
    ? `
      <div class="form-group">
        <label class="form-label">分享 Token</label>
        <div class="code-box">
          <span>${escHtml(share.token || '—')}</span>
          <button class="copy-btn" onclick="copyText(${JSON.stringify(share.token || '')})" title="复制">⎘</button>
        </div>
      </div>
    `
    : '';

  const shareBoxes = share
    ? `
      <div class="form-group">
        <label class="form-label">长链接</label>
        <div class="code-box">
          <span>${escHtml(share.url || '—')}</span>
          <button class="copy-btn" onclick="copyText(${JSON.stringify(share.url || '')})" title="复制">⎘</button>
        </div>
      </div>
      <div class="form-group">
        <label class="form-label">接口命令</label>
        <div class="code-box">
          <span>${escHtml(share.curl || '—')}</span>
          <button class="copy-btn" onclick="copyText(${JSON.stringify(share.curl || '')})" title="复制">⎘</button>
        </div>
      </div>
    `
    : `
      <div class="otp-share-empty">
        该邮箱当前还没有分享配置。你可以留空 token 自动生成，或填一个自定义 token 后保存。
      </div>
    `;

  return `
    <div class="otp-share-result">
      <div class="otp-share-meta">
        <span class="badge badge-gold">${mailbox.is_favorite ? '★ 收藏邮箱' : '邮箱已匹配'}</span>
        <span class="otp-share-address">${escHtml(mailbox.full_address || '—')}</span>
      </div>
      ${tokenBox}
      ${shareBoxes}
      <div class="form-hint">
        ${share
          ? `最近更新：${escHtml(formatDate(share.updated_at))}`
          : '分享链接只会读取该邮箱最新一封邮件里的 OTP，不会暴露账号 API Key。'}
      </div>
    </div>
  `;
}

async function renderAdminAccounts(container) {
  const actions = $('topbar-actions');
  if (actions) {
    actions.innerHTML = `<button class="btn btn-primary btn-sm" onclick="showCreateAccountModal()">+ 创建账户</button>`;
  }

  const [accounts, favoritePage, allPage] = await Promise.all([
    api.admin.listAccounts(),
    api.listMailboxesPage(1, 100, 'favorites').catch(() => ({ data: [] })),
    api.listMailboxesPage(1, 100, 'all').catch(() => ({ data: [] })),
  ]);
  const shareMailboxes = mergeAdminOTPShareMailboxes(favoritePage.data || [], allPage.data || []);
  state.adminOTPShare.mailboxes = shareMailboxes;
  if (state.adminOTPShare.mailbox?.id) {
    const refreshed = shareMailboxes.find(item => item.id === state.adminOTPShare.mailbox.id);
    if (refreshed) state.adminOTPShare.mailbox = refreshed;
  }

  container.innerHTML = `
    <div class="account-admin-grid">
      <div class="card">
        <div class="card-header">
          <div class="card-title">👥 账户列表</div>
          <div style="font-size:0.78rem;color:var(--text-muted)">共 ${(accounts||[]).length} 个账户</div>
        </div>
        <div class="table-wrap">
          <table class="admin-table stack-table">
            <thead>
              <tr><th>用户名</th><th>角色</th><th>创建时间</th><th>操作</th></tr>
            </thead>
            <tbody>
              ${(accounts||[]).map(a => `
                <tr>
                  ${buildDataLabel('用户名', `
                    <div style="font-weight:600">${escHtml(a.username || '—')}</div>
                    <div class="code-box" style="margin-top:0.3rem;font-size:0.72rem">
                      <span>${escHtml(a.api_key || '—')}</span>
                      <button class="copy-btn" onclick="copyText('${escHtml(a.api_key||'')}')">⎘</button>
                    </div>
                  `)}
                  ${buildDataLabel('角色', a.is_admin
                    ? '<span class="badge badge-gold">管理员</span>'
                    : '<span class="badge badge-gray">普通用户</span>')}
                  ${buildDataLabel('创建时间', formatDate(a.created_at), 'style="font-size:0.8rem"')}
                  ${buildDataLabel('操作', !a.is_admin
                    ? `<div class="table-actions"><button class="btn btn-danger btn-sm" onclick="confirmDeleteAccount('${a.id}','${escHtml(a.username||'')}')">删除</button></div>`
                    : '<span style="color:var(--text-muted)">—</span>')}
                </tr>
              `).join('')}
            </tbody>
          </table>
        </div>
      </div>

      <div class="card">
        <div class="card-header">
          <div>
            <div class="card-title">🔐 邮箱级 OTP 分享</div>
            <div style="font-size:0.78rem;color:var(--text-muted)">输入邮箱地址，生成或自定义分享 token，并输出长链接与取码命令</div>
          </div>
          <div style="font-size:0.78rem;color:var(--text-muted)">收藏建议 ${(favoritePage.data || []).length} 个</div>
        </div>
        <div class="card-body">
          <div class="form-group">
            <label class="form-label">邮箱地址</label>
            <input class="form-input" id="otp-share-address" list="otp-share-addresses" placeholder="name@example.com" value="${escHtml(state.adminOTPShare.address || '')}" />
            <datalist id="otp-share-addresses">${buildAdminOTPShareOptions(shareMailboxes)}</datalist>
            <div class="form-hint">支持手动输入；通常直接填你已收藏保存的邮箱。</div>
          </div>
          <div class="form-group">
            <label class="form-label">自定义 Token（可选）</label>
            <input class="form-input" id="otp-share-token" placeholder="留空则自动生成" value="${escHtml(state.adminOTPShare.token || '')}" />
            <div class="form-hint">允许 6-64 位字母、数字、下划线或短横线。</div>
          </div>
          <div class="otp-share-actions">
            <button class="btn btn-ghost btn-sm" onclick="loadAdminOTPShare()">读取当前分享</button>
            <button class="btn btn-primary btn-sm" onclick="saveAdminOTPShare()">保存分享</button>
            <button class="btn btn-danger btn-sm" onclick="deleteAdminOTPShare()">停用分享</button>
          </div>
          <div id="otp-share-result-wrap" style="margin-top:1rem">
            ${buildAdminOTPShareResult()}
          </div>
        </div>
      </div>
    </div>
  `;
}

async function resolveAdminOTPShareMailbox(requireExistingShare = false) {
  const address = String($('otp-share-address')?.value || '').trim().toLowerCase();
  const token = String($('otp-share-token')?.value || '').trim();
  state.adminOTPShare.address = address;
  state.adminOTPShare.token = token;
  if (!address) {
    toast('请输入邮箱地址', 'warn');
    return null;
  }
  try {
    const mailbox = await api.lookupMailboxByAddress(address);
    state.adminOTPShare.mailbox = mailbox;
    state.adminOTPShare.address = mailbox.full_address || address;
    return mailbox;
  } catch (e) {
    if (!requireExistingShare) state.adminOTPShare.share = null;
    toast('邮箱查找失败：' + e.message, 'error');
    return null;
  }
}

window.loadAdminOTPShare = async function() {
  const mailbox = await resolveAdminOTPShareMailbox(true);
  if (!mailbox) return;
  try {
    const share = await api.getMailboxOTPShare(mailbox.id);
    state.adminOTPShare.share = share;
    state.adminOTPShare.token = share.token || '';
    toast('已读取当前分享', 'success');
  } catch (e) {
    state.adminOTPShare.share = null;
    toast(e.message.includes('otp share not found') ? '该邮箱当前没有分享配置' : ('读取失败：' + e.message), e.message.includes('otp share not found') ? 'warn' : 'error');
  }
  navigate('admin-accounts');
};

window.saveAdminOTPShare = async function() {
  const mailbox = await resolveAdminOTPShareMailbox(false);
  if (!mailbox) return;
  try {
    const customRequested = !!state.adminOTPShare.token;
    const body = {};
    if (state.adminOTPShare.token) body.token = state.adminOTPShare.token;
    const share = await api.upsertMailboxOTPShare(mailbox.id, body);
    state.adminOTPShare.share = share;
    state.adminOTPShare.token = share.token || '';
    toast(customRequested ? 'OTP 分享已保存' : 'OTP 分享已生成', 'success');
    navigate('admin-accounts');
  } catch (e) {
    toast('保存失败：' + e.message, 'error');
  }
};

window.deleteAdminOTPShare = async function() {
  const mailbox = await resolveAdminOTPShareMailbox(true);
  if (!mailbox) return;
  try {
    await api.deleteMailboxOTPShare(mailbox.id);
    state.adminOTPShare.share = null;
    state.adminOTPShare.token = '';
    toast('OTP 分享已停用', 'success');
    navigate('admin-accounts');
  } catch (e) {
    toast('停用失败：' + e.message, 'error');
  }
};

window.showCreateAccountModal = function() {
  showModal('创建账户', `
    <div class="form-group">
      <label class="form-label">用户名</label>
      <input class="form-input" id="new-acc-username" placeholder="username" />
    </div>
    <div class="form-group">
      <label class="form-label">
        <input type="checkbox" id="new-acc-admin" style="margin-right:0.4rem">
        设为管理员
      </label>
    </div>
  `, async () => {
    const username = ($('new-acc-username')?.value || '').trim();
    if (!username) { toast('请输入用户名', 'warn'); return false; }
    const is_admin = $('new-acc-admin')?.checked || false;
    try {
      await api.admin.createAccount({ username, is_admin });
      toast('账户已创建', 'success');
      navigate('admin-accounts');
    } catch(e) { toast('创建失败: ' + e.message, 'error'); return false; }
  });
};

window.confirmDeleteAccount = function(id, name) {
  showModal('删除账户', `<p>确定删除账户 <strong>${escHtml(name)}</strong>？</p>`, async () => {
    try {
      await api.admin.deleteAccount(id);
      toast('账户已删除', 'success');
      navigate('admin-accounts');
    } catch(e) { toast('删除失败: ' + e.message, 'error'); }
  });
};

// ─── Admin: 域名管理 ─────────────────────────────────────────
async function renderAdminDomains(container) {
  const actions = $('topbar-actions');
  if (actions) {
    actions.innerHTML = `
      <button class="btn btn-primary btn-sm" onclick="showAddDomainModal()">+ 手动添加</button>
      <button class="btn btn-success btn-sm" onclick="showMXRegisterModal()" style="margin-left:0.4rem">⚡ MX 自动注册</button>
      <button class="btn btn-ghost btn-sm" onclick="showCFCreateModal()" style="margin-left:0.4rem">☁ CF 创建</button>
    `;
  }

  const [payload, settings] = await Promise.all([
    api.domainsPayload(),
    api.admin.getSettings().catch(() => ({})),
  ]);
  const domains = Array.isArray(payload) ? payload : (payload.domains || []);
  const summary = payload.summary || {
    total: domains.length,
    active: domains.filter(d => d.status === 'active').length,
    pending: domains.filter(d => d.status === 'pending').length,
    disabled: domains.filter(d => d.status === 'disabled').length,
  };
  const query = (state.adminDomainQuery || '').trim().toLowerCase();
  const statusFilter = state.adminDomainStatus || 'all';
  const hostnameFilter = state.adminDomainHostname || 'all';
  const hostnameOptions = [...new Set(domains.map(d => (d.hostname || '').trim()).filter(Boolean))].sort();
  const filtered = domains.filter(d => {
    if (statusFilter !== 'all' && d.status !== statusFilter) return false;
    if (hostnameFilter !== 'all' && (d.hostname || '') !== hostnameFilter) return false;
    if (query && !String(d.domain || '').toLowerCase().includes(query)) return false;
    return true;
  });
  const pending = filtered.filter(d => d.status === 'pending');
  const regular = filtered.filter(d => d.status !== 'pending');
  const selectedIds = Object.keys(state.adminDomainSelection || {}).filter(id => state.adminDomainSelection[id]);

  const apiSubEnabled = settings.api_subdomain_enabled === 'true' || settings.api_subdomain_enabled === true;
  const apiSubLength = Number(settings.api_subdomain_length || 5);
  const apiDomainStrategy = settings.api_domain_strategy || 'random';
  const apiDomainFixed = settings.api_domain_fixed || '';
  const activeForFixed = domains.filter(d => d.is_active);

  container.innerHTML = `
    <div class="admin-domain-page" style="max-width:1120px;display:flex;flex-direction:column;gap:1rem">
      <div class="card">
        <div class="card-body admin-domain-summary" style="display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:0.8rem">
          <div><div style="font-size:1.2rem;font-weight:700">${summary.total || 0}</div><div style="font-size:0.78rem;color:var(--text-muted)">总域名</div></div>
          <div><div style="font-size:1.2rem;font-weight:700">${summary.active || 0}</div><div style="font-size:0.78rem;color:var(--text-muted)">Active</div></div>
          <div><div style="font-size:1.2rem;font-weight:700">${summary.pending || 0}</div><div style="font-size:0.78rem;color:var(--text-muted)">Pending</div></div>
          <div><div style="font-size:1.2rem;font-weight:700">${summary.disabled || 0}</div><div style="font-size:0.78rem;color:var(--text-muted)">Disabled</div></div>
        </div>
      </div>

      <div class="card" style="border:1px solid rgba(34,197,94,.32);box-shadow:0 10px 28px rgba(34,197,94,.08)">
        <div class="card-header">
          <div class="card-title">⚙ API 创建邮箱默认配置</div>
          <div style="font-size:0.82rem;color:var(--text-muted)">客户端调用 <code>POST /api/mailboxes</code> 未显式传 <code>subdomain_mode</code>/<code>domain</code> 时使用。请求里的字段始终优先。</div>
        </div>
        <div class="card-body" style="display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:0.8rem">
          <div class="toggle-wrap" style="margin:0">
            <label class="toggle">
              <input type="checkbox" id="api-sub-enabled" ${apiSubEnabled ? 'checked' : ''}>
              <span class="toggle-slider"></span>
            </label>
            <div>
              <div class="toggle-label">默认启用多级子域名</div>
              <span class="toggle-desc" style="font-size:0.74rem">仅作用于已开启 <code>subdomain_enabled</code> 的域名；未开启的域名静默回退 v1。</span>
            </div>
          </div>
          <div class="form-group" style="margin:0">
            <label class="form-label">默认子域随机长度（2–8）</label>
            <input class="form-input" id="api-sub-length" type="number" min="2" max="8" value="${apiSubLength}" />
          </div>
          <div class="form-group" style="margin:0">
            <label class="form-label">域名选择策略</label>
            <select class="form-input" id="api-domain-strategy">
              <option value="random" ${apiDomainStrategy === 'random' ? 'selected' : ''}>随机选用已激活域名</option>
              <option value="fixed" ${apiDomainStrategy === 'fixed' ? 'selected' : ''}>指定固定域名</option>
            </select>
          </div>
          <div class="form-group" style="margin:0" id="api-domain-fixed-wrap" ${apiDomainStrategy === 'fixed' ? '' : 'hidden'}>
            <label class="form-label">指定的域名</label>
            <select class="form-input" id="api-domain-fixed">
              <option value="">— 选择一个 —</option>
              ${activeForFixed.map(d => `<option value="${escHtml(d.domain)}" ${apiDomainFixed === d.domain ? 'selected' : ''}>${escHtml(d.domain)}${d.subdomain_enabled ? ' ✦' : ''}</option>`).join('')}
            </select>
          </div>
        </div>
        <div class="card-body" style="padding-top:0;display:flex;gap:0.5rem;align-items:center">
          <button class="btn btn-primary btn-sm" onclick="saveApiMailboxDefaults()">✓ 保存配置</button>
          <span style="font-size:0.78rem;color:var(--text-muted)">回退规则：API 请求要求 v2，但所选域名未启用，自动回退 v1（响应里有 <code>fallback_reason</code>）。</span>
        </div>
      </div>

      <div class="card" style="border:1px solid rgba(99,102,241,.28);box-shadow:0 10px 28px rgba(99,102,241,.08)">
        <div class="card-header">
          <div class="card-title">☁ Cloudflare / 批量操作</div>
          <div style="font-size:0.82rem;color:var(--text-muted)">这里集中放显眼入口，避免只在表格里找不到。</div>
        </div>
        <div class="card-body" style="display:flex;flex-wrap:wrap;gap:0.6rem;align-items:center">
          <button class="btn btn-success btn-sm" onclick="showMXRegisterModal()">⚡ MX 自动注册</button>
          <button class="btn btn-primary btn-sm" onclick="showCFCreateModal()">☁ Cloudflare 创建 MX</button>
          <button class="btn btn-ghost btn-sm" onclick="showAddDomainModal()">+ 手动添加域名</button>
          <span style="font-size:0.78rem;color:var(--text-muted)">单个域名的 CF 删除在下方每行操作里；批量删除并删 CF 在“批量操作区”。</span>
        </div>
      </div>

      <div class="card">
        <div class="card-header">
          <div class="card-title">批量操作区</div>
          <div style="font-size:0.82rem;color:var(--text-muted)">先勾选域名，再使用这里的批量启用 / 停用 / 删除 / 子域入口。</div>
        </div>
        <div class="card-body" style="display:flex;gap:0.5rem;flex-wrap:wrap;align-items:center">
          <button class="btn btn-ghost btn-sm" onclick="toggleAllAdminDomains(true)">全选当前结果</button>
          <button class="btn btn-ghost btn-sm" onclick="toggleAllAdminDomains(false)">清空选择</button>
          <button class="btn btn-ghost btn-sm" id="admin-domains-bulk-enable" ${selectedIds.length ? '' : 'disabled'} onclick="batchToggleDomains(true)">批量启用</button>
          <button class="btn btn-ghost btn-sm" id="admin-domains-bulk-disable" ${selectedIds.length ? '' : 'disabled'} onclick="batchToggleDomains(false)">批量停用</button>
          <button class="btn btn-ghost btn-sm" id="admin-domains-bulk-sub-on" ${selectedIds.length ? '' : 'disabled'} onclick="batchToggleDomainSubdomain(true)" title="批量为选中域名打开子域开关">✦ 批量启用子域</button>
          <button class="btn btn-ghost btn-sm" id="admin-domains-bulk-sub-off" ${selectedIds.length ? '' : 'disabled'} onclick="batchToggleDomainSubdomain(false)">关闭子域</button>
          <button class="btn btn-danger btn-sm" id="admin-domains-bulk-delete-local" ${selectedIds.length ? '' : 'disabled'} onclick="confirmBatchDeleteDomains(false)">批量删除本地</button>
          <button class="btn btn-danger btn-sm" id="admin-domains-bulk-delete-cf" ${selectedIds.length ? '' : 'disabled'} onclick="confirmBatchDeleteDomains(true)">批量删除并删 CF</button>
          <span id="admin-domains-bulk-status" data-filtered-count="${filtered.length}" style="font-size:0.78rem;color:var(--text-muted)">已选 ${selectedIds.length} 个，当前结果 ${filtered.length} 个</span>
        </div>
      </div>

      <div class="card">
        <div class="card-body admin-domain-filters" style="display:grid;grid-template-columns:2fr 1fr 1fr;gap:0.8rem;align-items:end">
          <div class="form-group" style="margin:0">
            <label class="form-label">搜索域名</label>
            <input class="form-input" value="${escHtml(state.adminDomainQuery || '')}" placeholder="按域名关键字过滤" oninput="adminDomainSetFilter('query', this.value)" />
          </div>
          <div class="form-group" style="margin:0">
            <label class="form-label">状态筛选</label>
            <select class="form-input" onchange="adminDomainSetFilter('status', this.value)">
              <option value="all" ${statusFilter === 'all' ? 'selected' : ''}>全部状态</option>
              <option value="active" ${statusFilter === 'active' ? 'selected' : ''}>active</option>
              <option value="pending" ${statusFilter === 'pending' ? 'selected' : ''}>pending</option>
              <option value="disabled" ${statusFilter === 'disabled' ? 'selected' : ''}>disabled</option>
            </select>
          </div>
          <div class="form-group" style="margin:0">
            <label class="form-label">Hostname 筛选</label>
            <select class="form-input" onchange="adminDomainSetFilter('hostname', this.value)">
              <option value="all">全部 Hostname</option>
              ${hostnameOptions.map(h => `<option value="${escHtml(h)}" ${hostnameFilter === h ? 'selected' : ''}>${escHtml(h)}</option>`).join('')}
            </select>
          </div>
        </div>
      </div>

      ${pending.length > 0 ? `
        <div class="card" style="border-left:3px solid var(--clr-warn,#e6a817)">
          <div class="card-header">
            <div class="card-title">🔄 待 MX 验证 (${pending.length})</div>
            <div style="font-size:0.78rem;color:var(--text-muted)">后台每 30 秒自动检测，验证通过后自动加入域名池</div>
          </div>
          <div class="table-wrap">
            <table class="admin-table stack-table">
              <thead><tr><th></th><th>域名</th><th>Hostname</th><th>子域</th><th>上次检测</th><th>操作</th></tr></thead>
              <tbody id="pending-domains-tbody">
                ${pending.map(d => `
                  <tr id="pending-row-${d.id}">
                    ${buildDataLabel('选择', `<input type="checkbox" ${state.adminDomainSelection[d.id] ? 'checked' : ''} onchange="toggleAdminDomainSelection(${d.id}, this.checked)" />`)}
                    ${buildDataLabel('域名', `<span style="font-family:var(--font-mono)">${escHtml(d.domain)}</span>`)}
                    ${buildDataLabel('Hostname', `<span style="font-family:var(--font-mono);font-size:0.82rem;color:var(--text-secondary)">${escHtml(d.hostname || '—')}</span>`)}
                    ${buildDataLabel('子域', d.subdomain_enabled
                      ? `<span class="badge badge-green">✦ 开 / ${Number(d.subdomain_random_length || 5)}</span>`
                      : '<span class="badge badge-gray">关</span>')}
                    ${buildDataLabel('上次检测', d.mx_checked_at ? timeAgo(d.mx_checked_at) : '从未', 'style="font-size:0.78rem"')}
                    ${buildDataLabel('操作', `
                      <div class="table-actions">
                        <span class="badge badge-gold" id="pending-status-${d.id}">⏳ 检测中</span>
                        <button class="btn btn-ghost btn-sm" onclick="showEditDomainHostnameModal(${d.id},'${escHtml(d.domain)}',${d.hostname_id ?? 'null'},'${escHtml(d.hostname || '')}')">Hostname</button>
                        <button class="btn btn-ghost btn-sm" onclick="showEditDomainSubdomainModal(${d.id},'${escHtml(d.domain)}',${d.subdomain_enabled ? 'true' : 'false'},${Number(d.subdomain_random_length || 5)})">Subdomain</button>
                        <button class="btn btn-danger btn-sm" onclick="confirmCFDeleteDomain(${d.id},'${escHtml(d.domain)}')">CF 删除</button>
                        <button class="btn btn-danger btn-sm" onclick="confirmDeleteDomain(${d.id},'${escHtml(d.domain)}')">✕</button>
                      </div>
                    `)}
                  </tr>
                `).join('')}
              </tbody>
            </table>
          </div>
        </div>
      ` : ''}

      <div class="card">
        <div class="card-header">
          <div class="card-title">🌐 域名列表</div>
          <div style="font-size:0.78rem;color:var(--text-muted)">共 ${regular.length} 个</div>
        </div>
        <div class="table-wrap">
          <table class="admin-table stack-table">
            <thead><tr><th></th><th>域名</th><th>Hostname</th><th>子域</th><th>状态</th><th>操作</th></tr></thead>
            <tbody>
              ${regular.length === 0 ? `<tr><td colspan="6" style="text-align:center;color:var(--text-muted)">暂无域名</td></tr>` :
                regular.map(d => `
                  <tr>
                    ${buildDataLabel('选择', `<input type="checkbox" ${state.adminDomainSelection[d.id] ? 'checked' : ''} onchange="toggleAdminDomainSelection(${d.id}, this.checked)" />`)}
                    ${buildDataLabel('域名', `<span style="font-family:var(--font-mono)">${escHtml(d.domain)}</span>`)}
                    ${buildDataLabel('Hostname', `<span style="font-family:var(--font-mono);font-size:0.82rem;color:var(--text-secondary)">${escHtml(d.hostname || '—')}</span>`)}
                    ${buildDataLabel('子域', d.subdomain_enabled
                      ? `<span class="badge badge-green">✦ 开 / ${Number(d.subdomain_random_length || 5)}</span>`
                      : '<span class="badge badge-gray">关</span>')}
                    ${buildDataLabel('状态', d.is_active
                      ? '<span class="badge badge-green">● 启用</span>'
                      : '<span class="badge badge-gray">○ 停用</span>')}
                    ${buildDataLabel('操作', `
                      <div class="table-actions">
                        <button class="btn btn-ghost btn-sm" onclick="showEditDomainHostnameModal(${d.id},'${escHtml(d.domain)}',${d.hostname_id ?? 'null'},'${escHtml(d.hostname || '')}')">Hostname</button>
                        <button class="btn btn-ghost btn-sm" onclick="showEditDomainSubdomainModal(${d.id},'${escHtml(d.domain)}',${d.subdomain_enabled ? 'true' : 'false'},${Number(d.subdomain_random_length || 5)})">Subdomain</button>
                        <button class="btn btn-ghost btn-sm" onclick="toggleDomain(${d.id},${!d.is_active})">${d.is_active ? '停用' : '启用'}</button>
                        <button class="btn btn-danger btn-sm" onclick="confirmCFDeleteDomain(${d.id},'${escHtml(d.domain)}')">CF 删除</button>
                        <button class="btn btn-danger btn-sm" onclick="confirmDeleteDomain(${d.id},'${escHtml(d.domain)}')">删除</button>
                      </div>
                    `)}
                  </tr>
                `).join('')}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  `;

  // API 默认配置：策略切换时显示/隐藏固定域名字段
  const stratSel = container.querySelector('#api-domain-strategy');
  const fixedWrap = container.querySelector('#api-domain-fixed-wrap');
  if (stratSel && fixedWrap) {
    stratSel.addEventListener('change', () => {
      if (stratSel.value === 'fixed') fixedWrap.removeAttribute('hidden');
      else fixedWrap.setAttribute('hidden', '');
    });
  }

  // 如果有 pending 域名，开始轮询
  if (pending.length > 0) {
    startPendingDomainPoller(pending.map(d => d.id));
  }
}

window.saveApiMailboxDefaults = async function() {
  const enabled = !!document.getElementById('api-sub-enabled')?.checked;
  const lenRaw = Number(document.getElementById('api-sub-length')?.value);
  const length = Number.isFinite(lenRaw) ? Math.max(2, Math.min(8, Math.floor(lenRaw))) : 5;
  const strategy = document.getElementById('api-domain-strategy')?.value || 'random';
  let fixed = (document.getElementById('api-domain-fixed')?.value || '').trim();
  if (strategy !== 'fixed') fixed = '';
  if (strategy === 'fixed' && !fixed) {
    toast('请选择指定的域名', 'warn');
    return;
  }
  try {
    await api.admin.saveSettings({
      api_subdomain_enabled: enabled ? 'true' : 'false',
      api_subdomain_length: String(length),
      api_domain_strategy: strategy,
      api_domain_fixed: fixed,
    });
    toast('API 默认配置已保存', 'success');
  } catch(e) {
    toast('保存失败：' + e.message, 'error');
  }
};

window.batchToggleDomainSubdomain = async function(enabled) {
  const ids = Object.keys(state.adminDomainSelection || {}).filter(id => state.adminDomainSelection[id]).map(Number);
  if (!ids.length) return;
  try {
    await api.admin.batchToggleDomainsSubdomain(ids, !!enabled);
    toast(enabled ? '已批量启用子域' : '已批量关闭子域', 'success');
    navigate('admin-domains');
  } catch(e) { toast('批量操作失败: ' + e.message, 'error'); }
};

window.adminDomainSetFilter = function(kind, value) {
  if (kind === 'query') state.adminDomainQuery = value;
  if (kind === 'status') state.adminDomainStatus = value;
  if (kind === 'hostname') state.adminDomainHostname = value;
  renderPage('admin-domains');
};

window.toggleAdminDomainSelection = function(id, checked) {
  state.adminDomainSelection[id] = !!checked;
  updateAdminDomainBulkActions();
};

window.toggleAllAdminDomains = async function(checked) {
  const payload = await api.domainsPayload();
  const domains = Array.isArray(payload) ? payload : (payload.domains || []);
  const query = (state.adminDomainQuery || '').trim().toLowerCase();
  const statusFilter = state.adminDomainStatus || 'all';
  const hostnameFilter = state.adminDomainHostname || 'all';
  domains.forEach(d => {
    const match = (statusFilter === 'all' || d.status === statusFilter) &&
      (hostnameFilter === 'all' || (d.hostname || '') === hostnameFilter) &&
      (!query || String(d.domain || '').toLowerCase().includes(query));
    if (match) state.adminDomainSelection[d.id] = !!checked;
  });
  renderPage('admin-domains');
};

function updateAdminDomainBulkActions() {
  const selectedCount = Object.keys(state.adminDomainSelection || {}).filter(id => state.adminDomainSelection[id]).length;
  [
    'admin-domains-bulk-enable',
    'admin-domains-bulk-disable',
    'admin-domains-bulk-sub-on',
    'admin-domains-bulk-sub-off',
    'admin-domains-bulk-delete-local',
    'admin-domains-bulk-delete-cf',
  ].forEach(id => {
    const btn = $(id);
    if (btn) btn.disabled = selectedCount === 0;
  });

  const status = $('admin-domains-bulk-status');
  if (status) {
    const filteredCount = Number(status.dataset.filteredCount || 0);
    status.textContent = `已选 ${selectedCount} 个，当前结果 ${filteredCount} 个`;
  }
}

window.showAddDomainModal = function() {
  const old = document.querySelector('.modal-overlay');
  if (old) old.remove();

  let serverIP = '';
  let defaultHostname = '';
  let activeHostnames = [];

  const overlay = el('div', 'modal-overlay');
  overlay.innerHTML = `
    <div class="modal" style="max-width:580px">
      <div class="modal-title">添加域名</div>
      <button class="modal-close" onclick="this.closest('.modal-overlay').remove()">✕</button>

      <div id="add-step1">
        <div class="form-group" style="margin-bottom:0.5rem">
          <label class="form-label">域名</label>
          <input class="form-input" id="add-domain-inp" placeholder="example.com" autofocus />
          <div class="form-hint">输入将用于接收邮件的顶级域名</div>
        </div>
        <div class="form-group" style="margin-bottom:0.5rem">
          <label class="form-label">域名 Hostname（可选）</label>
          <select class="form-input" id="add-hostname-sel"></select>
          <div class="form-hint">从已录入的 hostname 中选择；不指定时跟随默认 hostname 或回退 <code>mail.&lt;domain&gt;</code></div>
        </div>
        <div class="toggle-wrap" style="margin:0.4rem 0 0.6rem">
          <label class="toggle">
            <input type="checkbox" id="add-subdomain-enabled">
            <span class="toggle-slider"></span>
          </label>
          <div>
            <div class="toggle-label">启用多级子域名</div>
            <span class="toggle-desc" style="font-size:0.74rem">勾选后 DNS 提示将增加通配 <code>*</code> MX 记录；CF 创建时会同步建通配 MX。</span>
          </div>
        </div>
        <div id="add-dns-hint" style="background:var(--bg-secondary);border-radius:6px;padding:0.7rem 0.9rem;margin-bottom:0.8rem;font-size:0.8rem">
          <b>需要配置的 DNS 记录：</b>
          <table style="margin-top:0.5rem;width:100%;border-collapse:collapse;font-size:0.76rem">
            <thead><tr><th style="text-align:left;padding:2px 5px">类型</th><th style="text-align:left;padding:2px 5px">主机名</th><th style="text-align:left;padding:2px 5px">内容</th><th style="text-align:left;padding:2px 5px">优先级</th></tr></thead>
            <tbody id="add-dns-rows"></tbody>
          </table>
        </div>
        <div id="add-mx-result" style="display:none;margin-bottom:0.7rem"></div>
        <div class="modal-actions" id="add-actions">
          <button class="btn btn-ghost" onclick="this.closest('.modal-overlay').remove()">取消</button>
          <button class="btn btn-secondary" id="add-check-btn" onclick="doAddDomainCheck(false)">🔍 检测 MX</button>
          <button class="btn btn-primary"  id="add-force-btn" style="display:none" onclick="doAddDomainCheck(true)">⚡ 强制添加</button>
        </div>
      </div>

      <div id="add-step2" style="display:none"></div>
    </div>
  `;
  document.body.appendChild(overlay);
  overlay.addEventListener('click', e => { if (e.target === overlay) overlay.remove(); });

  const inp = overlay.querySelector('#add-domain-inp');
  const hostnameSel = overlay.querySelector('#add-hostname-sel');
  const subEnabledInp = overlay.querySelector('#add-subdomain-enabled');
  inp?.addEventListener('keydown', e => { if (e.key === 'Enter') window.doAddDomainCheck(false); });
  inp?.addEventListener('input', updateDnsHint);
  hostnameSel?.addEventListener('change', updateDnsHint);
  subEnabledInp?.addEventListener('change', updateDnsHint);

  function renderHostnameOptions() {
    if (!hostnameSel) return;
    hostnameSel.innerHTML = buildHostnameOptions(activeHostnames, '', {
      allowBlank: true,
      blankLabel: defaultHostname
        ? `跟随默认（当前 ${defaultHostname}）`
        : '不指定（回退 mail.<domain>）',
    });
  }

  function updateDnsHint() {
    const d = (inp?.value || '').trim() || 'example.com';
    const ip = serverIP || '&lt;服务器IP&gt;';
    const selectedHostname = findHostnameById(activeHostnames, hostnameSel?.value)?.hostname || '';
    const hn = selectedHostname || defaultHostname || 'mail.' + d;
    const hasHostname = !!(selectedHostname || defaultHostname);
    const subEnabled = !!subEnabledInp?.checked;
    const tbody = document.getElementById('add-dns-rows');
    if (!tbody) return;
    tbody.innerHTML = `
      <tr><td style="padding:2px 5px">MX</td><td style="padding:2px 5px;font-family:monospace">@</td><td style="padding:2px 5px;font-family:monospace">${escHtml(hn)}</td><td style="padding:2px 5px">10</td></tr>
      ${hasHostname ? '' : `<tr><td style="padding:2px 5px">A</td><td style="padding:2px 5px;font-family:monospace">mail.${escHtml(d)}</td><td style="padding:2px 5px;font-family:monospace">${escHtml(ip)}</td><td style="padding:2px 5px">—</td></tr>`}
      <tr><td style="padding:2px 5px">TXT</td><td style="padding:2px 5px;font-family:monospace">@</td><td style="padding:2px 5px;font-family:monospace">v=spf1 ip4:${escHtml(ip)} ~all</td><td style="padding:2px 5px">—</td></tr>
      ${subEnabled ? `<tr style="background:rgba(34,197,94,.06)"><td style="padding:2px 5px">MX</td><td style="padding:2px 5px;font-family:monospace">*</td><td style="padding:2px 5px;font-family:monospace">${escHtml(hn)}</td><td style="padding:2px 5px">10</td></tr>` : ''}
    `;
  }
  api.publicSettings().then(s => {
    serverIP = s.smtp_server_ip || '';
    defaultHostname = getDefaultHostnameFromSettings(s);
    activeHostnames = getActiveHostnamesFromSettings(s);
    renderHostnameOptions();
    updateDnsHint();
  }).catch(() => {
    renderHostnameOptions();
    updateDnsHint();
  });
  renderHostnameOptions();
  updateDnsHint();

  window.doAddDomainCheck = async function(force) {
    const domain = (inp?.value || '').trim().toLowerCase();
    const hostnameId = Number(hostnameSel?.value || 0) || 0;
    const subEnabled = !!subEnabledInp?.checked;
    if (!domain) { toast('请输入域名', 'warn'); return; }
    const checkBtn = document.getElementById('add-check-btn');
    const forceBtn = document.getElementById('add-force-btn');
    const resEl    = document.getElementById('add-mx-result');
    if (checkBtn) { checkBtn.disabled = true; checkBtn.textContent = '检测中...'; }

    try {
      if (force) {
        // 强制直接添加（跳过 MX 检测）
        const body = { domain, subdomain_enabled: subEnabled };
        if (hostnameId > 0) body.hostname_id = hostnameId;
        const r = await api.admin.addDomain(body);
        showDnsInstructions(domain, r);
        overlay.remove();
        return;
      }

      // 先做 MX 检测（force:false）
      let r;
      try {
        const body = { domain, force: false, subdomain_enabled: subEnabled };
        if (hostnameId > 0) body.hostname_id = hostnameId;
        r = await api.admin.mxImport(body);
        // MX 通过 → 已添加
        const step1 = document.getElementById('add-step1');
        const step2 = document.getElementById('add-step2');
        if (step1) step1.style.display = 'none';
        if (step2) {
          step2.style.display = 'block';
          step2.innerHTML = `
            <div style="text-align:center;padding:1.2rem 0">
              <div style="font-size:2rem">✅</div>
              <h3 style="margin:0.5rem 0">MX 验证通过</h3>
              <p style="font-size:0.84rem;color:var(--text-secondary)">域名 <strong>${escHtml(domain)}</strong> 已立即加入域名池</p>
              <button class="btn btn-primary" style="margin-top:1rem" onclick="this.closest('.modal-overlay').remove();navigate('admin-domains')">查看域名列表</button>
            </div>`;
        }
        toast('✓ ' + domain + ' MX 验证通过，已加入域名池', 'success');
      } catch(err) {
        // MX 未通过 → 提示强制添加选项
        if (checkBtn) { checkBtn.disabled = false; checkBtn.textContent = '🔍 检测 MX'; }
        if (forceBtn) forceBtn.style.display = '';
        if (resEl) {
          resEl.style.display = 'block';
          resEl.innerHTML = `
            <div style="background:var(--clr-warn-bg,#fff8e1);border:1px solid var(--clr-warn,#e6a817);border-radius:6px;padding:0.6rem 0.9rem;font-size:0.82rem">
              ⚠️ <b>MX 记录未检测到</b>：${escHtml(err.message)}<br>
              <span style="color:var(--text-muted)">请先配置上方 DNS 记录后重新检测，或点击「强制添加」跳过检测直接加入域名池</span>
            </div>`;
        }
      }
    } catch(e) {
      if (checkBtn) { checkBtn.disabled = false; checkBtn.textContent = '🔍 检测 MX'; }
      toast('操作失败: ' + e.message, 'error');
    }
  };
};

// \u5c55\u793a\u6dfb\u52a0\u57df\u540d\u540e\u7684 DNS \u914d\u7f6e\u6307\u5f15
function showDnsInstructions(domain, result) {
  const dns = result.dns_records || [];
  const rows = dns.map(r => `
    <tr>
      <td style="padding:3px 8px;font-weight:600">${escHtml(r.type)}</td>
      <td style="padding:3px 8px">${escHtml(r.host)}</td>
      <td style="padding:3px 8px;font-family:monospace;font-size:0.78rem">${escHtml(r.value)}</td>
      <td style="padding:3px 8px">${r.priority || '\u2014'}</td>
    </tr>`).join('');
  const old = document.querySelector('.modal-overlay');
  if (old) old.remove();
  const overlay = el('div', 'modal-overlay');
  overlay.innerHTML = `
    <div class="modal" style="max-width:600px">
      <div class="modal-title">\u2705 \u57df\u540d\u5df2\u6dfb\u52a0\uff1a${escHtml(domain)}</div>
      <p style="font-size:0.84rem;color:var(--text-secondary);margin:0.5rem 0 0.8rem">
        \u8bf7\u5728 DNS \u7ba1\u7406\u9762\u677f\u6dfb\u52a0\u4ee5\u4e0b\u8bb0\u5f55\uff0c\u4e00\u822c 5\u201330 \u5206\u949f\u751f\u6548\uff1a
      </p>
      <div class="table-wrap">
        <table>
          <thead><tr><th>\u7c7b\u578b</th><th>\u4e3b\u673a\u540d</th><th>\u5185\u5bb9</th><th>\u4f18\u5148\u7ea7</th></tr></thead>
          <tbody>${rows}</tbody>
        </table>
      </div>
      <p style="font-size:0.78rem;color:var(--text-muted);margin-top:0.6rem">\u2139\ufe0f ${escHtml(result.instructions || '')}</p>
      <div class="modal-actions">
        <button class="btn btn-primary" onclick="this.closest('.modal-overlay').remove();navigate('admin-domains')">
          \u5b8c\u6210\uff0c\u67e5\u770b\u57df\u540d\u5217\u8868
        </button>
      </div>
    </div>
  `;
  document.body.appendChild(overlay);
  overlay.addEventListener('click', e => { if (e.target === overlay) { overlay.remove(); navigate('admin-domains'); }});
}

window.toggleDomain = async function(id, newActive) {
  try {
    await api.admin.toggleDomain(id, newActive);
    toast('状态已切换', 'success');
    navigate('admin-domains');
  } catch(e) { toast('操作失败: ' + e.message, 'error'); }
};

window.confirmDeleteDomain = function(id, name) {
  showModal('删除域名', `<p>确定删除域名 <strong>${escHtml(name)}</strong>？</p>`, async () => {
    try {
      await api.admin.deleteDomain(id);
      toast('域名已删除', 'success');
      navigate('admin-domains');
    } catch(e) { toast('删除失败: ' + e.message, 'error'); }
  });
};

window.confirmCFDeleteDomain = function(id, name) {
  showModal('CF 删除域名', `<p>确定删除域名 <strong>${escHtml(name)}</strong> 并联动删除 Cloudflare MX 记录？</p><p style="font-size:0.8rem;color:var(--clr-danger);margin-top:0.5rem">⚠ 将同时删除本地域名及其下所有邮箱和邮件。</p>`, async () => {
    try {
      await api.admin.deleteDomainCF(id);
      toast('域名及 Cloudflare MX 已删除', 'success');
      navigate('admin-domains');
    } catch(e) { toast('删除失败: ' + e.message, 'error'); }
  });
};

window.showEditDomainHostnameModal = async function(id, domain, hostnameId, hostname) {
  let hostnames = [];
  try {
    hostnames = await api.admin.listHostnames();
  } catch (e) {
    toast('读取 Hostname 列表失败：' + e.message, 'error');
    return;
  }

  const currentId = Number(hostnameId) || 0;
  const hasCurrent = currentId > 0 && !!findHostnameById(hostnames, currentId);
  const options = buildHostnameOptions(hostnames, hasCurrent ? currentId : '', {
    allowBlank: true,
    blankLabel: hostname ? `跟随默认（当前绑定 ${hostname}）` : '跟随默认 Hostname',
  });

  showModal('编辑域名 Hostname', `
    <div class="form-group">
      <label class="form-label">域名</label>
      <input class="form-input" value="${escHtml(domain)}" disabled />
    </div>
    <div class="form-group">
      <label class="form-label">Hostname（可选）</label>
      <select class="form-input" id="edit-domain-hostname">${options}</select>
      <div class="form-hint">从已录入的 hostname 中选择；留空则跟随默认 hostname。</div>
    </div>
  `, async () => {
    try {
      const value = Number($('edit-domain-hostname')?.value || 0) || 0;
      const body = value > 0 ? { hostname_id: value } : { hostname: '' };
      await api.admin.updateDomainHostname(id, body);
      toast('Hostname 已更新', 'success');
      navigate('admin-domains');
    } catch(e) { toast('更新失败: ' + e.message, 'error'); return false; }
  });
};

window.showEditDomainSubdomainModal = function(id, domain, enabled, length) {
  const len = Math.max(2, Math.min(8, Number(length) || 5));
  showModal('编辑多级子域名', `
    <div class="form-group">
      <label class="form-label">域名</label>
      <input class="form-input" value="${escHtml(domain)}" disabled />
    </div>
    <div class="toggle-wrap" style="margin:0.4rem 0">
      <label class="toggle">
        <input type="checkbox" id="edit-sub-enabled" ${enabled ? 'checked' : ''}>
        <span class="toggle-slider"></span>
      </label>
      <div>
        <div class="toggle-label">启用多级子域名</div>
        <span class="toggle-desc" style="font-size:0.74rem">开启后，需要在该域名 DNS 中配置 <code>*.${escHtml(domain)}</code> 通配 MX 记录。</span>
      </div>
    </div>
    <div class="form-group">
      <label class="form-label">随机子域长度（2–8）</label>
      <input class="form-input" id="edit-sub-length" type="number" min="2" max="8" value="${len}" />
      <div class="form-hint">用户/API 未显式指定长度时使用</div>
    </div>
  `, async () => {
    const enabledNew = !!document.getElementById('edit-sub-enabled')?.checked;
    const rawLen = Number(document.getElementById('edit-sub-length')?.value);
    const lenNew = Number.isFinite(rawLen) ? Math.max(2, Math.min(8, Math.floor(rawLen))) : len;
    try {
      await api.admin.updateDomainSubdomain(id, { enabled: enabledNew, random_length: lenNew });
      toast('子域设置已更新', 'success');
      navigate('admin-domains');
    } catch(e) { toast('更新失败: ' + e.message, 'error'); return false; }
  });
};

window.batchToggleDomains = async function(active) {
  const ids = Object.keys(state.adminDomainSelection || {}).filter(id => state.adminDomainSelection[id]).map(Number);
  if (!ids.length) return;
  try {
    await api.admin.batchToggleDomains(ids, active);
    toast(active ? '已批量启用' : '已批量停用', 'success');
    navigate('admin-domains');
  } catch(e) { toast('批量操作失败: ' + e.message, 'error'); }
};

window.confirmBatchDeleteDomains = function(deleteCloudflare) {
  const ids = Object.keys(state.adminDomainSelection || {}).filter(id => state.adminDomainSelection[id]).map(Number);
  if (!ids.length) return;
  const title = deleteCloudflare ? '批量删除并联动 CF' : '批量删除域名';
  const body = deleteCloudflare
    ? `<p>确定删除选中的 <strong>${ids.length}</strong> 个域名，并联动删除 Cloudflare MX？</p><p style="font-size:0.8rem;color:var(--clr-danger);margin-top:0.5rem">⚠ 将同时删除本地域名及其下所有邮箱和邮件。</p>`
    : `<p>确定删除选中的 <strong>${ids.length}</strong> 个域名？</p>`;
  showModal(title, body, async () => {
    try {
      const res = await api.admin.batchDeleteDomains(ids, deleteCloudflare);
      const failed = (res.results || []).filter(r => r.error).length;
      toast(failed ? `批量完成，成功 ${res.deleted}，失败 ${failed}` : `已批量删除 ${res.deleted} 个域名`, failed ? 'warn' : 'success');
      state.adminDomainSelection = {};
      navigate('admin-domains');
    } catch(e) { toast('批量删除失败: ' + e.message, 'error'); }
  });
};

window.showCFCreateModal = function() {
  const old = document.querySelector('.modal-overlay');
  if (old) old.remove();
  let activeHostnames = [];
  const overlay = el('div', 'modal-overlay');
  overlay.innerHTML = `
    <div class="modal" style="max-width:560px">
      <div class="modal-title">☁ Cloudflare 自动创建 MX</div>
      <button class="modal-close" onclick="this.closest('.modal-overlay').remove()">✕</button>
      <div class="form-group">
        <label class="form-label">完整域名</label>
        <input class="form-input" id="cfc-domain" placeholder="sub.example.com" autofocus />
      </div>
      <div class="form-group">
        <label class="form-label">MX Hostname</label>
        <select class="form-input" id="cfc-hostname"></select>
        <div class="form-hint">从系统里已录入并启用的 hostname 中选择一个作为 MX 目标。</div>
      </div>
      <div class="form-group">
        <label class="form-label">Cloudflare Zone（可选）</label>
        <input class="form-input" id="cfc-zone" placeholder="example.com" />
      </div>
      <div class="toggle-wrap" style="margin:0.4rem 0">
        <label class="toggle">
          <input type="checkbox" id="cfc-subdomain-enabled">
          <span class="toggle-slider"></span>
        </label>
        <div>
          <div class="toggle-label">同时启用多级子域名</div>
          <span class="toggle-desc" style="font-size:0.74rem">勾选后将额外为该域名创建通配 <code>*</code> MX 记录，并在数据库标记 <code>subdomain_enabled</code>。</span>
        </div>
      </div>
      <div class="modal-actions">
        <button class="btn btn-ghost" onclick="this.closest('.modal-overlay').remove()">取消</button>
        <button class="btn btn-primary" id="cfc-submit">创建</button>
      </div>
    </div>
  `;
  document.body.appendChild(overlay);
  overlay.addEventListener('click', e => { if (e.target === overlay) overlay.remove(); });
  const hostnameSel = overlay.querySelector('#cfc-hostname');
  const renderHostnameOptions = () => {
    if (!hostnameSel) return;
    if (!activeHostnames.length) {
      hostnameSel.innerHTML = `<option value="">请先在系统设置中添加并启用 Hostname</option>`;
      hostnameSel.disabled = true;
      return;
    }
    hostnameSel.disabled = false;
    hostnameSel.innerHTML = buildHostnameOptions(activeHostnames, activeHostnames[0]?.id || '', { allowBlank: false });
  };
  api.hostnames().then(list => {
    activeHostnames = normalizeHostnameList(list).filter(item => item.is_active);
    renderHostnameOptions();
  }).catch(() => renderHostnameOptions());
  renderHostnameOptions();
  overlay.querySelector('#cfc-submit').addEventListener('click', async () => {
    const btn = overlay.querySelector('#cfc-submit');
    const domain = (overlay.querySelector('#cfc-domain')?.value || '').trim().toLowerCase();
    const hostnameId = Number(overlay.querySelector('#cfc-hostname')?.value || 0) || 0;
    const zone = (overlay.querySelector('#cfc-zone')?.value || '').trim();
    const subEnabled = !!overlay.querySelector('#cfc-subdomain-enabled')?.checked;
    if (!domain || !hostnameId) { toast('请选择域名和 Hostname', 'warn'); return; }
    btn.disabled = true;
    btn.textContent = '创建中...';
    try {
      const body = { domain, hostname_id: hostnameId, subdomain_enabled: subEnabled };
      if (zone) body.zone = zone;
      await api.admin.cfCreateDomain(body);
      toast(subEnabled ? 'Cloudflare MX（含通配）已创建，域名已入池' : 'Cloudflare MX 已创建，域名已入池', 'success');
      overlay.remove();
      navigate('admin-domains');
    } catch(e) {
      btn.disabled = false;
      btn.textContent = '创建';
      toast('创建失败: ' + e.message, 'error');
    }
  });
};

// ─── Admin: 系统设置 ─────────────────────────────────────────
async function renderAdminSettings(container) {
  let settings = {};
  let hostnameItems = [];
  try {
    [settings, hostnameItems] = await Promise.all([
      api.admin.getSettings().catch(() => ({})),
      api.admin.listHostnames().catch(() => []),
    ]);
  } catch {}

  const regOpen    = settings.registration_open === 'true' || settings.registration_open === true;
  const smtpIp      = settings.smtp_server_ip       || '';
  const smtpHostname = getDefaultHostnameFromSettings(settings);
  const hostnames = normalizeHostnameList(hostnameItems);
  const activeHostnames = hostnames.filter(item => item.is_active);
  const siteTitle  = settings.site_title            || 'TempMail';
  const defDomain  = settings.default_domain        || '';
  const ttlMins    = settings.mailbox_ttl_minutes   || '30';
  const mailboxPageSize = settings.mailbox_page_size || String(DEFAULT_MAILBOX_PAGE_SIZE);
  const apiTtlMins = settings.api_mailbox_ttl_minutes || '';
  const catchallEnabled = settings.catchall_enabled === 'true' || settings.catchall_enabled === true;
  const catchallAccountId = settings.catchall_account_id || '';
  const cfApiToken = settings.cf_api_token || '';
  const announce   = settings.announcement          || '';
  const maxMb      = settings.max_mailboxes_per_user|| '5';
  const tgBotToken = settings.tg_bot_token || '';
  const tgChatId = settings.tg_chat_id || '';
  const tgThreadId = settings.tg_message_thread_id || '';
  const tgForwardMode = settings.tg_forward_mode || 'all_with_attachments';
  const hostnameRows = hostnames.length === 0
    ? `<tr><td colspan="4" style="text-align:center;color:var(--text-muted)">还没有录入任何 Hostname</td></tr>`
    : hostnames.map(item => `
        <tr>
          ${buildDataLabel('Hostname', `<span style="font-family:var(--font-mono)">${escHtml(item.hostname)}</span>`)}
          ${buildDataLabel('状态', item.is_active ? '<span class="badge badge-green">● 启用</span>' : '<span class="badge badge-gray">○ 停用</span>')}
          ${buildDataLabel('绑定域名', String(item.domain_count || 0))}
          ${buildDataLabel('操作', `
            <div class="table-actions">
              <button class="btn btn-ghost btn-sm" onclick="editHostnameSetting(${item.id},'${escHtml(item.hostname)}')">编辑</button>
              <button class="btn btn-ghost btn-sm" onclick="toggleHostnameSetting(${item.id},${item.is_active ? 'false' : 'true'})">${item.is_active ? '停用' : '启用'}</button>
              <button class="btn btn-danger btn-sm" onclick="deleteHostnameSetting(${item.id},'${escHtml(item.hostname)}',${Number(item.domain_count || 0)})">删除</button>
            </div>
          `)}
        </tr>
      `).join('');

  function inputRow(id, label, value, hint, placeholder = '', settingKey = '') {
    const key = settingKey || id.replace(/^input-/, '').replace(/-/g, '_');
    return `
      <div class="form-group">
        <label class="form-label">${label}</label>
        <div style="display:flex;gap:0.5rem">
          <input class="form-input" id="${id}" value="${escHtml(value)}" placeholder="${escHtml(placeholder)}" style="flex:1" />
          <button class="btn btn-primary btn-sm" onclick="saveSetting('${id}','${key}')">✓ 保存</button>
        </div>
        ${hint ? `<div class="form-hint">${hint}</div>` : ''}
      </div>`;
  }

  function selectRow(id, label, value, options, hint, settingKey = '') {
    const key = settingKey || id.replace(/^input-/, '').replace(/-/g, '_');
    return `
      <div class="form-group">
        <label class="form-label">${label}</label>
        <div style="display:flex;gap:0.5rem">
          <select class="form-input" id="${id}" style="flex:1">
            ${options.map(opt => `<option value="${escHtml(opt.value)}" ${opt.value === value ? 'selected' : ''}>${escHtml(opt.label)}</option>`).join('')}
          </select>
          <button class="btn btn-primary btn-sm" onclick="saveSetting('${id}','${key}')">✓ 保存</button>
        </div>
        ${hint ? `<div class="form-hint">${hint}</div>` : ''}
      </div>`;
  }

  container.innerHTML = `
    <div class="card" style="max-width:980px">
      <div class="card-header"><div class="card-title">⚙ 系统设置</div></div>
      <div class="card-body" style="display:flex;flex-direction:column;gap:0.1rem">

        <!-- 注册开关 -->
        <div class="toggle-wrap" style="margin-bottom:0.5rem">
          <label class="toggle">
            <input type="checkbox" id="toggle-reg" ${regOpen ? 'checked' : ''} onchange="saveRegistrationSetting(this.checked)">
            <span class="toggle-slider"></span>
          </label>
          <div>
            <div class="toggle-label">开放自行注册</div>
            <span class="toggle-desc">开启后未登录用户可在登录页自行注册账户</span>
          </div>
        </div>
        <div class="divider"></div>

        <!-- 站点名称 -->
        ${inputRow('input-site-title', '站点名称', siteTitle, '显示在标题栏和登录页', 'TempMail')}
        <div class="divider"></div>

        <!-- 公告 -->
        <div class="form-group">
          <label class="form-label">公告内容</label>
          <div style="display:flex;gap:0.5rem">
            <textarea class="form-input" id="input-announcement" rows="2" placeholder="留空则不显示公告" style="flex:1;resize:vertical">${escHtml(announce)}</textarea>
            <button class="btn btn-primary btn-sm" onclick="saveSetting('input-announcement','announcement')" style="align-self:flex-start">✓ 保存</button>
          </div>
          <div class="form-hint">显示在已登录用户的 Dashboard 顶部</div>
        </div>
        <div class="divider"></div>

        <!-- SMTP IP -->
        ${inputRow('input-smtp-ip', 'SMTP 服务器公网 IP', smtpIp, '用于生成 SPF DNS 配置提示', '0.0.0.0', 'smtp_server_ip')}
        <div class="divider"></div>

        <div class="form-group">
          <label class="form-label">Hostname 管理</label>
          <div style="display:flex;gap:0.5rem;align-items:flex-start;flex-wrap:wrap">
            <input class="form-input" id="input-new-hostname" placeholder="mail.yourdomain.com" style="flex:1;min-width:260px" />
            <button class="btn btn-primary btn-sm" onclick="addHostnameSetting()">+ 添加 Hostname</button>
          </div>
          <div class="form-hint">这里维护可选的 MX Hostname。域名添加、MX 验证、Cloudflare 创建和域名编辑都会从这里下拉选择。</div>
          <div class="table-wrap" style="margin-top:0.75rem">
            <table class="admin-table stack-table">
              <thead><tr><th>Hostname</th><th>状态</th><th>绑定域名</th><th>操作</th></tr></thead>
              <tbody>${hostnameRows}</tbody>
            </table>
          </div>
        </div>
        <div class="divider"></div>

        <!-- 默认邮箱域名 -->
        ${inputRow('input-default-domain', '默认邮箱域名', defDomain, '创建邮箱时下拉框优先选中的域名', 'mail.example.com')}
        <div class="divider"></div>

        <!-- 邮箱 TTL（前端创建） -->
        ${inputRow('input-mailbox-ttl-minutes', '邮箱有效期（分钟）', ttlMins, '前端 Web UI 创建邮箱的默认存活时间，0 = 永不过期', '30')}
        <div class="divider"></div>

        <!-- Dashboard 邮箱分页大小 -->
        ${inputRow('input-mailbox-page-size', '收藏夹/邮箱列表每页显示数量', mailboxPageSize, '默认 6。只改变每页显示数量，不改变面板总高度；调少后底部会留空。建议范围 1-24。', '6', 'mailbox_page_size')}
        <div class="divider"></div>

        <!-- 邮箱 TTL（API 创建） -->
        ${inputRow('input-api-mailbox-ttl-minutes', 'API 创建邮箱有效期（分钟）', apiTtlMins, '通过 API 创建邮箱时的存活时间。留空 = 复用上方"邮箱有效期"，0 = 永不过期', '留空则与上方一致')}
        <div class="divider"></div>

        <!-- Catch-all -->
        <div class="toggle-wrap" style="margin-bottom:0.5rem">
          <label class="toggle">
            <input type="checkbox" id="toggle-catchall" ${catchallEnabled ? 'checked' : ''} onchange="saveCatchAllSetting(this.checked)">
            <span class="toggle-slider"></span>
          </label>
          <div>
            <div class="toggle-label">开启 Catch-all 自动建箱</div>
            <span class="toggle-desc">收到未知地址邮件时自动创建邮箱并落到指定账号名下</span>
          </div>
        </div>
        ${inputRow('input-catchall-account-id', 'Catch-all 归属账号 ID', catchallAccountId, '留空则自动归属首个管理员账号', '留空使用首个管理员')}
        <div class="divider"></div>

        <div class="form-group">
          <label class="form-label">Cloudflare API Token</label>
          <div style="display:flex;gap:0.5rem">
            <input class="form-input" id="input-cf-api-token" type="password" value="${escHtml(cfApiToken)}" placeholder="填写具有 Zone:DNS:Edit 权限的 Token" style="flex:1" />
            <button class="btn btn-primary btn-sm" onclick="saveSetting('input-cf-api-token','cf_api_token')">✓ 保存</button>
          </div>
          <div class="form-hint">仅管理员可用；用于自动创建/删除 Cloudflare MX 记录。</div>
        </div>
        <div class="divider"></div>

        <div class="form-group">
          <label class="form-label">Telegram Bot Token</label>
          <div style="display:flex;gap:0.5rem">
            <input class="form-input" id="input-tg-bot-token" type="password" value="${escHtml(tgBotToken)}" placeholder="123456:ABC..." style="flex:1" />
            <button class="btn btn-primary btn-sm" onclick="saveSetting('input-tg-bot-token','tg_bot_token')">✓ 保存</button>
          </div>
          <div class="form-hint">用于将收件通知转发到 Telegram Bot。</div>
        </div>
        <div class="divider"></div>

        ${inputRow('input-tg-chat-id', 'Telegram Chat ID', tgChatId, '用户、群组或频道 ID，例如 <code>123456789</code> 或 <code>-1001234567890</code>。', '123456789', 'tg_chat_id')}
        <div class="divider"></div>

        ${inputRow('input-tg-thread-id', 'Telegram Topic / Thread ID', tgThreadId, '可选。群组开启话题时可指定发送到某个 topic。留空则发到默认会话。', '留空可选', 'tg_message_thread_id')}
        <div class="divider"></div>

        ${selectRow('input-tg-forward-mode', 'Telegram 转发模式', tgForwardMode, [
          { value: 'subject_only', label: '仅转发标题' },
          { value: 'important_without_attachments', label: '重要正文（不带附件）' },
          { value: 'important_with_attachments', label: '重要正文（带附件）' },
          { value: 'notify_all', label: '仅通知所有邮件' },
          { value: 'all_with_attachments', label: '完整正文（带附件）' },
          { value: 'all_without_attachments', label: '完整正文（不带附件）' },
          { value: 'attachments_only', label: '仅转发带附件邮件（带附件）' },
          { value: 'notify_attachments', label: '仅通知有带附件邮件' },
        ], '每个邮箱可单独开启或关闭 TG 转发；重点正文模式会尽量提取 OTP、验证/登录类链接和较短的关键信息，同时关闭 Telegram 链接预览卡片。', 'tg_forward_mode')}
        <div class="divider"></div>

        <div class="form-group">
          <label class="form-label">Telegram 测试</label>
          <div style="display:flex;gap:0.5rem;align-items:center;flex-wrap:wrap">
            <button class="btn btn-primary btn-sm" onclick="testTelegramForward()">发送测试消息</button>
            <span class="form-hint" style="margin:0">请先保存上面的 Token、Chat ID 和模式设置。</span>
          </div>
        </div>
        <div class="divider"></div>

        <!-- 每用户邮箱上限 -->
        ${inputRow('input-max-mailboxes-per-user', '每账户邮箱上限', maxMb, '每个账户同时存在的邮箱数量上限', '5')}
        <div class="divider"></div>

        <!-- 服务信息 -->
        <div style="font-size:0.82rem;color:var(--text-secondary)">
          <strong>服务信息</strong>
          <p style="margin-top:0.5rem;line-height:2">
            SMTP IP:&nbsp;<code>${escHtml(smtpIp||'<未设置>')}</code><br>
            默认 Hostname:&nbsp;<code>${escHtml(smtpHostname||'<未设置>')}</code><br>
            已启用 Hostname:&nbsp;<code>${escHtml(activeHostnames.map(item => item.hostname).join(', ') || '<无>')}</code><br>
            API:&nbsp;<code>${window.location.origin}/api</code><br>
            前端:&nbsp;<code>${window.location.origin}</code>
          </p>
        </div>
        <div class="divider"></div>

        <!-- 管理员 Key -->
        <div>
          <div class="form-label">管理员 API Key</div>
          <div class="code-box" style="font-size:0.78rem">
            <span style="filter:blur(4px);cursor:pointer" onclick="this.style.filter='none'">${escHtml(state.apiKey)}</span>
            <button class="copy-btn" onclick="copyText('${escHtml(state.apiKey)}')">⎘</button>
          </div>
          <div class="form-hint">Key 文件位置：<code>/data/admin.key</code>（API 服务容器内）</div>
        </div>

      </div>
    </div>
  `;
}

// 通用保存
window.saveSetting = async function(inputId, settingKey) {
  const el2 = document.getElementById(inputId);
  let val = el2 ? (el2.tagName === 'TEXTAREA' ? el2.value : el2.value.trim()) : '';
  if (settingKey === 'mailbox_page_size') {
    const parsed = Math.floor(Number(val));
    if (!Number.isFinite(parsed) || parsed < MIN_MAILBOX_PAGE_SIZE || parsed > MAX_MAILBOX_PAGE_SIZE) {
      toast(`请输入 ${MIN_MAILBOX_PAGE_SIZE} 到 ${MAX_MAILBOX_PAGE_SIZE} 之间的整数`, 'warn');
      return;
    }
    val = String(parsed);
    state.mailboxPageSize = parsed;
  }
  try {
    await api.admin.saveSettings({ [settingKey]: val });
    toast('已保存', 'success');
  } catch(e) { toast('保存失败: ' + e.message, 'error'); }
};

// 兼容旧调用
window.saveSmtpIp = async function() { await window.saveSetting('input-smtp-ip', 'smtp_server_ip'); };

window.addHostnameSetting = async function() {
  const value = ($('input-new-hostname')?.value || '').trim();
  if (!value) { toast('请输入 Hostname', 'warn'); return; }
  try {
    await api.admin.addHostname({ hostname: value });
    toast('Hostname 已添加', 'success');
    navigate('admin-settings');
  } catch (e) {
    toast('添加失败：' + e.message, 'error');
  }
};

window.editHostnameSetting = function(id, hostname) {
  showModal('编辑 Hostname', `
    <div class="form-group">
      <label class="form-label">Hostname</label>
      <input class="form-input" id="edit-hostname-value" value="${escHtml(hostname || '')}" placeholder="mail.yourdomain.com" />
      <div class="form-hint">修改后，已绑定该 Hostname 的域名会同步更新。</div>
    </div>
  `, async () => {
    const value = ($('edit-hostname-value')?.value || '').trim();
    if (!value) { toast('请输入 Hostname', 'warn'); return false; }
    try {
      await api.admin.updateHostname(id, { hostname: value });
      toast('Hostname 已更新', 'success');
      navigate('admin-settings');
    } catch (e) {
      toast('更新失败：' + e.message, 'error');
      return false;
    }
  });
};

window.toggleHostnameSetting = async function(id, active) {
  try {
    await api.admin.toggleHostname(id, !!active);
    toast(active ? 'Hostname 已启用' : 'Hostname 已停用', 'success');
    navigate('admin-settings');
  } catch (e) {
    toast('操作失败：' + e.message, 'error');
  }
};

window.deleteHostnameSetting = function(id, hostname, domainCount) {
  const extra = Number(domainCount || 0) > 0
    ? `<p style="font-size:0.8rem;color:var(--clr-danger);margin-top:0.5rem">⚠ 删除后会把绑定它的 ${Number(domainCount)} 个域名清空为“跟随默认 Hostname”。</p>`
    : '';
  showModal('删除 Hostname', `<p>确定删除 Hostname <strong>${escHtml(hostname)}</strong>？</p>${extra}`, async () => {
    try {
      const res = await api.admin.deleteHostname(id);
      const cleared = Number(res.cleared_domains || 0);
      toast(cleared > 0 ? `Hostname 已删除，并清空了 ${cleared} 个域名的绑定` : 'Hostname 已删除', 'success');
      navigate('admin-settings');
    } catch (e) {
      toast('删除失败：' + e.message, 'error');
      return false;
    }
  });
};

window.saveRegistrationSetting = async function(enabled) {
  try {
    await api.admin.saveSettings({ registration_open: enabled ? 'true' : 'false' });
    toast(`注册已${enabled ? '开启' : '关闭'}`, 'success');
  } catch(e) {
    toast('保存失败: ' + e.message, 'error');
    const cb = $('toggle-reg');
    if (cb) cb.checked = !enabled;
  }
};

window.saveCatchAllSetting = async function(enabled) {
  try {
    await api.admin.saveSettings({ catchall_enabled: enabled ? 'true' : 'false' });
    toast(`Catch-all 已${enabled ? '开启' : '关闭'}`, 'success');
  } catch(e) {
    toast('保存失败: ' + e.message, 'error');
    const cb = $('toggle-catchall');
    if (cb) cb.checked = !enabled;
  }
};

window.testTelegramForward = async function() {
  try {
    await api.admin.testTelegram();
    toast('TG 测试消息已发送，请到 Telegram 查看', 'success');
  } catch (e) {
    toast('TG 测试失败：' + e.message, 'error');
  }
};

// ─── Modal ────────────────────────────────────────────────
function showModal(title, bodyHtml, onConfirm) {
  const old = document.querySelector('.modal-overlay');
  if (old) old.remove();

  const overlay = el('div', 'modal-overlay');
  overlay.innerHTML = `
    <div class="modal">
      <div class="modal-title">${escHtml(title)}</div>
      <button class="modal-close" onclick="this.closest('.modal-overlay').remove()">✕</button>
      ${bodyHtml}
      <div class="modal-actions">
        <button class="btn btn-ghost" onclick="this.closest('.modal-overlay').remove()">取消</button>
        <button class="btn btn-primary" id="modal-confirm-btn">确认</button>
      </div>
    </div>
  `;
  document.body.appendChild(overlay);
  overlay.addEventListener('click', e => { if (e.target === overlay) overlay.remove(); });

  const confirmBtn = overlay.querySelector('#modal-confirm-btn');
  confirmBtn.addEventListener('click', async () => {
    confirmBtn.disabled = true;
    const result = await onConfirm();
    if (result !== false) overlay.remove();
    else confirmBtn.disabled = false;
  });
}

// ─── MX 自动注册（全自动验证流程）──────────────────────────
// 轮询待验证域名状态
let _pendingPollerTimer = null;
let _inboxPollerTimer   = null;
function clearInboxPoller() {
  if (_inboxPollerTimer) { clearInterval(_inboxPollerTimer); _inboxPollerTimer = null; }
}
function startPendingDomainPoller(ids) {
  if (!ids || ids.length === 0) return;
  clearInterval(_pendingPollerTimer);
  const remaining = new Set(ids);
  _pendingPollerTimer = setInterval(async () => {
    for (const id of [...remaining]) {
      try {
        const d = await api.getDomainStatus(id); // 使用非管理员接口
        const statusEl = document.getElementById('pending-status-' + id);
        const rowEl    = document.getElementById('pending-row-'   + id);
        if (d.status === 'active') {
          if (statusEl) statusEl.innerHTML = '<span class="badge badge-green">✓ 已激活</span>';
          remaining.delete(id);
          toast(`✓ 域名 ${d.domain} MX验证通过，已加入域名池`, 'success');
          setTimeout(() => { if (rowEl) rowEl.remove(); }, 3000);
        } else if (statusEl) {
          const ago = d.mx_checked_at ? timeAgo(d.mx_checked_at) : '从未';
          statusEl.innerHTML = `<span class="badge badge-gold">⏳ 检测中（上次${ago}）</span>`;
        }
      } catch {}
    }
    if (remaining.size === 0) clearInterval(_pendingPollerTimer);
  }, 5000);
}

window.showMXRegisterModal = function() {
  const isAdmin = !!state.account?.is_admin;
  const old = document.querySelector('.modal-overlay');
  if (old) old.remove();
  let defaultHostname = '';
  let activeHostnames = [];
  const overlay = el('div', 'modal-overlay');
  overlay.innerHTML = `
    <div class="modal" style="max-width:560px">
      <div class="modal-title">⚡ MX 自动注册域名</div>
      <button class="modal-close" onclick="this.closest('.modal-overlay').remove()">✕</button>
      <p style="font-size:0.82rem;color:var(--text-secondary);margin:0.5rem 0 0.8rem">
        提交域名后系统立即检测 MX 记录。若已配置则直接激活；
        否则进入待验证队列，后台每 <b>30 秒</b>自动重试，无需手动确认。
      </p>
      <div class="form-group">
        <label class="form-label">域名（如 example.com）</label>
        <input class="form-input" id="mxr-domain" placeholder="example.com" autofocus />
      </div>
      <div class="form-group">
        <label class="form-label">Hostname（可选）</label>
        <select class="form-input" id="mxr-hostname"></select>
        <div class="form-hint">从已录入的 hostname 中选择；留空则跟随默认 hostname 或回退 <code>mail.&lt;domain&gt;</code>。</div>
      </div>
      <div class="toggle-wrap" style="margin:0.4rem 0 0.6rem">
        <label class="toggle">
          <input type="checkbox" id="mxr-sub-enabled">
          <span class="toggle-slider"></span>
        </label>
        <div>
          <div class="toggle-label">启用多级子域名</div>
          <span class="toggle-desc" style="font-size:0.74rem">开启后会额外要求 <code>*.domain</code> 的通配 MX 生效，才能完成自动验证。</span>
        </div>
      </div>
      <div class="form-group" id="mxr-sub-length-wrap" style="display:none">
        <label class="form-label">随机子域长度（2–8）</label>
        <input class="form-input" id="mxr-sub-length" type="number" min="2" max="8" value="5" />
        <div class="form-hint">用于后续随机生成多级子域邮箱地址时的默认长度。</div>
      </div>
      <div id="mxr-dns-hint" style="display:none;background:var(--bg-secondary);border-radius:6px;padding:0.7rem 0.9rem;margin-bottom:0.6rem;font-size:0.8rem">
        <b>请在 DNS 管理面板添加以下记录：</b>
        <table style="margin-top:0.5rem;width:100%;border-collapse:collapse;font-size:0.76rem">
          <thead><tr><th style="text-align:left">类型</th><th style="text-align:left">主机名</th><th style="text-align:left">内容</th><th style="text-align:left">优先级</th></tr></thead>
          <tbody id="mxr-dns-rows"></tbody>
        </table>
      </div>
      <div id="mxr-status" style="display:none;margin-bottom:0.7rem"></div>
      <div class="modal-actions" id="mxr-actions">
        <button class="btn btn-ghost" onclick="this.closest('.modal-overlay').remove()">取消</button>
        <button class="btn btn-primary" id="mxr-submit">提交检测</button>
      </div>
    </div>
  `;
  document.body.appendChild(overlay);
  overlay.addEventListener('click', e => { if (e.target === overlay) overlay.remove(); });

  // 实时更新 DNS 提示
  const inp = overlay.querySelector('#mxr-domain');
  const hostnameSel = overlay.querySelector('#mxr-hostname');
  const subEnabledInp = overlay.querySelector('#mxr-sub-enabled');
  const subLengthWrap = overlay.querySelector('#mxr-sub-length-wrap');
  inp?.addEventListener('keydown', e => { if (e.key === 'Enter') submitMXRegister(); });
  hostnameSel?.addEventListener('change', () => {});
  subEnabledInp?.addEventListener('change', () => {
    if (subLengthWrap) subLengthWrap.style.display = subEnabledInp.checked ? '' : 'none';
  });

  const renderHostnameOptions = () => {
    if (!hostnameSel) return;
    hostnameSel.innerHTML = buildHostnameOptions(activeHostnames, '', {
      allowBlank: true,
      blankLabel: defaultHostname
        ? `跟随默认（当前 ${defaultHostname}）`
        : '不指定（回退 mail.<domain>）',
    });
  };
  api.publicSettings().then(s => {
    defaultHostname = getDefaultHostnameFromSettings(s);
    activeHostnames = getActiveHostnamesFromSettings(s);
    renderHostnameOptions();
  }).catch(() => renderHostnameOptions());
  renderHostnameOptions();

  overlay.querySelector('#mxr-submit').addEventListener('click', submitMXRegister);

  async function submitMXRegister() {
    const domain = (inp?.value || '').trim().toLowerCase();
    const hostnameId = Number(overlay.querySelector('#mxr-hostname')?.value || 0) || 0;
    const subEnabled = !!overlay.querySelector('#mxr-sub-enabled')?.checked;
    const subLengthRaw = Number(overlay.querySelector('#mxr-sub-length')?.value || 5);
    const subLength = Number.isFinite(subLengthRaw) ? Math.max(2, Math.min(8, Math.floor(subLengthRaw))) : 5;
    if (!domain) { toast('请输入域名', 'warn'); return; }
    const btn    = overlay.querySelector('#mxr-submit');
    const status = overlay.querySelector('#mxr-status');
    const hint   = overlay.querySelector('#mxr-dns-hint');
    btn.disabled = true;
    btn.textContent = '检测中...';
    status.style.display = 'none';

    const domainListPage = state.account?.is_admin ? 'admin-domains' : 'domains-guide';
    try {
      const body = { domain };
      if (hostnameId > 0) body.hostname_id = hostnameId;
      if (subEnabled) {
        body.subdomain_enabled = true;
        body.subdomain_random_length = subLength;
      }
      const r = isAdmin ? await api.admin.mxRegister(body) : await api.submitDomain(body);
      if (r.status === 'active') {
        overlay.innerHTML = `
          <div class="modal" style="text-align:center;padding:2rem">
            <div style="font-size:2rem">✅</div>
            <h3 style="margin:0.5rem 0">MX 验证通过</h3>
            <p style="font-size:0.84rem;color:var(--text-secondary)">域名 <strong>${escHtml(domain)}</strong> 已立即加入域名池</p>
            <button class="btn btn-primary" style="margin-top:1rem" onclick="this.closest('.modal-overlay').remove();navigate('${domainListPage}')">查看域名列表</button>
          </div>
        `;
        toast(`✓ ${domain} 已激活`, 'success');
      } else {
        // pending — 显示 DNS 配置 + 等待提示
        const dnsRecords = [
          ...(Array.isArray(r.dns_required) ? r.dns_required : []),
          ...(subEnabled && Array.isArray(r.wildcard_required) ? r.wildcard_required : []),
        ];
        const rows = dnsRecords.map(rec =>
          `<tr><td>${escHtml(rec.type)}</td><td style="font-family:monospace">${escHtml(rec.host)}</td><td style="font-family:monospace">${escHtml(rec.value)}</td><td>${rec.priority || '—'}</td></tr>`
        ).join('');
        overlay.querySelector('#mxr-dns-rows').innerHTML = rows;
        hint.style.display = 'block';

        const wildcardNote = subEnabled
          ? `<br><span style="color:var(--text-muted)">通配 MX：${escHtml(r.wildcard_mx_status || '等待检测')}</span>`
          : '';
        status.style.display = 'block';
        status.innerHTML = `
          <div style="background:var(--clr-warn-bg,#fff8e1);border:1px solid var(--clr-warn,#e6a817);border-radius:6px;padding:0.6rem 0.9rem;font-size:0.81rem">
            ⏳ <b>域名已加入验证队列（ID ${r.domain.id}）</b><br>
            MX 记录配置生效后（通常 5-30 分钟），系统将自动激活。${wildcardNote}<br>
            <span style="color:var(--text-muted)">此窗口关闭后可在「域名列表」页查看验证进度</span>
          </div>
        `;
        const actionsEl = overlay.querySelector('#mxr-actions');
        actionsEl.innerHTML = `<button class="btn btn-primary" onclick="this.closest('.modal-overlay').remove();navigate('${domainListPage}')">前往域名列表查看进度</button>`;

        // 开始在当前 overlay 内轮询
        startInlinePoller(r.domain.id, domain, overlay);
      }
    } catch(e) {
      btn.disabled = false;
      btn.textContent = '重新提交';
      status.style.display = 'block';
      status.innerHTML = `<div style="color:var(--clr-danger);font-size:0.82rem">❌ ${escHtml(e.message)}</div>`;
    }
  }

  async function startInlinePoller(domainId, domainName, modal) {
    const statusEl = modal.querySelector('#mxr-status');
    let attempts = 0;
    const timer = setInterval(async () => {
      attempts++;
      if (!document.body.contains(modal)) { clearInterval(timer); return; }
      try {
        const d = await api.getDomainStatus(domainId); // 非管理员接口
        if (d.status === 'active') {
          clearInterval(timer);
          if (statusEl) statusEl.innerHTML = `
            <div style="background:#e8f5e9;border:1px solid #4caf50;border-radius:6px;padding:0.6rem 0.9rem;font-size:0.81rem">
              ✅ <b>MX 验证通过！域名 ${escHtml(domainName)} 已自动激活。</b>
            </div>`;
          toast(`✓ ${domainName} 已自动激活`, 'success');
          setTimeout(() => { modal.remove(); navigate(state.account?.is_admin ? 'admin-domains' : 'domains-guide'); }, 2500);
        } else if (statusEl) {
          const ago = d.mx_checked_at ? timeAgo(d.mx_checked_at) : '从未';
          statusEl.innerHTML = `
            <div style="background:var(--clr-warn-bg,#fff8e1);border:1px solid var(--clr-warn,#e6a817);border-radius:6px;padding:0.6rem 0.9rem;font-size:0.81rem">
              ⏳ 等待中（第 ${attempts} 次检测，上次 ${ago}）…
            </div>`;
        }
      } catch {}
    }, 5000);
  }
};

// ─── API 文档 ─────────────────────────────────────────
function renderApiDocs(container) {
  const key = state.apiKey || 'YOUR_API_KEY';
  const base = window.location.origin;
  const sections = [
    {
      title: '🔐 认证方式',
      desc: '所有 /api/* 接口需要在 HTTP Header 中携带 API Key：',
      code: `# Bearer Token 方式
curl -H "Authorization: Bearer ${key}" ${base}/api/me

# Query 参数方式
curl "${base}/api/me?api_key=${key}"`,
    },
    {
      title: '📫 1. 创建临时邮箱',
      desc: 'POST /api/mailboxes — address 和 domain 均为可选字段',
      code: `# 随机地址 + 随机域名
curl -s -X POST ${base}/api/mailboxes \\
  -H "Authorization: Bearer ${key}" \\
  -H "Content-Type: application/json" \\
  -d '{}'

# 指定本地部分（@ 之前），域名随机
curl -s -X POST ${base}/api/mailboxes \\
  -H "Authorization: Bearer ${key}" \\
  -H "Content-Type: application/json" \\
  -d '{"address": "mytestbox"}'

# 指定域名，地址随机（domain 须是已激活域名）
curl -s -X POST ${base}/api/mailboxes \\
  -H "Authorization: Bearer ${key}" \\
  -H "Content-Type: application/json" \\
  -d '{"domain": "example.com"}'

# 同时指定地址和域名
curl -s -X POST ${base}/api/mailboxes \\
  -H "Authorization: Bearer ${key}" \\
  -H "Content-Type: application/json" \\
  -d '{"address": "mytestbox", "domain": "example.com"}'

# 错误码：
#   400 → domain 不存在或未激活
#   409 → 地址已被占用（换一个 address 或留空让系统随机生成）
#   503 → 系统内无可用域名`,
    },
    {
      title: '📌 2. 获取邮箱列表',
      desc: 'GET /api/mailboxes — 获取当前账号下所有邮箱',
      code: `curl -s ${base}/api/mailboxes \\
  -H "Authorization: Bearer ${key}"

# 分页
 curl -s "${base}/api/mailboxes?page=1&size=20" \\
  -H "Authorization: Bearer ${key}"`,
    },
    {
      title: '📥 3. 获取邮箱收件箱（邮件列表）',
      desc: 'GET /api/mailboxes/:id/emails — 按收件时间倒序列出邮件摘要',
      code: `MAILBOX_ID="你的邮箱UUID"
curl -s ${base}/api/mailboxes/$MAILBOX_ID/emails \\
  -H "Authorization: Bearer ${key}"

# 分页
curl -s "${base}/api/mailboxes/$MAILBOX_ID/emails?page=1&size=20" \\
  -H "Authorization: Bearer ${key}"`,
    },
    {
      title: '📝 4. 读取单封邮件',
      desc: 'GET /api/mailboxes/:id/emails/:email_id — 获取邮件完整内容（含 HTML/纯文本和原始数据）',
      code: `MAILBOX_ID="你的邮箱UUID"
EMAIL_ID="你的邮件UUID"
curl -s ${base}/api/mailboxes/$MAILBOX_ID/emails/$EMAIL_ID \\
  -H "Authorization: Bearer ${key}"`,
    },
    {
      title: '🔢 5. 提取最新一封邮件 OTP',
      desc: 'GET /api/mailboxes/:id/otp/latest — 直接返回该邮箱最新邮件中的验证码',
      code: `MAILBOX_ID="你的邮箱UUID"
curl -s ${base}/api/mailboxes/$MAILBOX_ID/otp/latest \\
  -H "Authorization: Bearer ${key}"

# 可能返回：
#   200 → {"otp":{"code":"528193",...}}
#   404 → 邮箱不存在 / 无邮件
#   422 → 最新邮件未识别到 OTP`,
    },
    {
      title: '🗑 6. 删除邮箱',
      desc: 'DELETE /api/mailboxes/:id — 立即删除邮箱及其所有邮件',
      code: `MAILBOX_ID="你的邮箱UUID"
curl -s -X DELETE ${base}/api/mailboxes/$MAILBOX_ID \\
  -H "Authorization: Bearer ${key}"`,
    },
    {
      title: '🗑 7. 删除单封邮件',
      desc: 'DELETE /api/mailboxes/:id/emails/:email_id',
      code: `curl -s -X DELETE ${base}/api/mailboxes/$MAILBOX_ID/emails/$EMAIL_ID \\
  -H "Authorization: Bearer ${key}"`,
    },
    {
      title: '☁ 8. Cloudflare / 域名管理（管理员）',
      desc: '新增的管理员域名接口示例',
      code: `# 查看可选 hostname 列表
curl -s ${base}/api/admin/hostnames \\
  -H "Authorization: Bearer ${key}"

# 更新域名 hostname
curl -s -X PUT ${base}/api/admin/domains/12/hostname \\
  -H "Authorization: Bearer ${key}" \\
  -H "Content-Type: application/json" \\
  -d '{"hostname_id":1}'

# 通过 Cloudflare 创建 MX 并加入域名池
curl -s -X POST ${base}/api/admin/domains/cf-create \\
  -H "Authorization: Bearer ${key}" \\
  -H "Content-Type: application/json" \\
  -d '{"domain":"sub.example.com","hostname_id":1}'

# 删除 Cloudflare MX 并删除本地域名
curl -s -X DELETE ${base}/api/admin/domains/12/cf \\
  -H "Authorization: Bearer ${key}"

# 批量启停
curl -s -X PUT ${base}/api/admin/domains/batch/toggle \\
  -H "Authorization: Bearer ${key}" \\
  -H "Content-Type: application/json" \\
  -d '{"ids":[1,2,3],"active":true}'

# 批量删除（可选联动 Cloudflare）
curl -s -X PUT ${base}/api/admin/domains/batch/delete \\
  -H "Authorization: Bearer ${key}" \\
  -H "Content-Type: application/json" \\
  -d '{"ids":[1,2,3],"delete_cloudflare":true}'

# 域名筛选
curl -s "${base}/api/domains?status=active&hostname=mail.example.com&q=example" \\
  -H "Authorization: Bearer ${key}"`,
    },
    {
      title: '🧪 9. 完整自动化示例（Shell 脚本）',
      desc: '创建邮箱 → 等待 5 秒 → 读取邮件 → 清理',
      code: `#!/bin/bash
BASE="${base}"
KEY="${key}"

# 1. 创建临时邮箱
MB=$(curl -s -X POST $BASE/api/mailboxes \\
  -H "Authorization: Bearer $KEY" \\
  -H "Content-Type: application/json" \\
  -d '{}')
MB_ID=$(echo $MB | python3 -c "import sys,json; print(json.load(sys.stdin)['mailbox']['id'])")
MB_ADDR=$(echo $MB | python3 -c "import sys,json; print(json.load(sys.stdin)['mailbox']['full_address'])")
echo "✓ 邮箱: $MB_ADDR (主键: $MB_ID)"

# 2. 向邮箱发送邮件...
echo "将测试邮件发到: $MB_ADDR"
sleep 5

# 3. 查看收件筱
EMAILS=$(curl -s $BASE/api/mailboxes/$MB_ID/emails \\
  -H "Authorization: Bearer $KEY")
echo "取到邮件: $EMAILS" | python3 -m json.tool

# 4. 读取第一封邮件（收件箱）
EMAIL_ID=$(echo $EMAILS | python3 -c "import sys,json;d=json.load(sys.stdin);print(d['data'][0]['id']) if d.get('data') else print('')" 2>/dev/null)
if [ -n "$EMAIL_ID" ]; then
  curl -s $BASE/api/mailboxes/$MB_ID/emails/$EMAIL_ID \\
    -H "Authorization: Bearer $KEY" | python3 -m json.tool
fi

# 5. 删除邮箱
curl -s -X DELETE $BASE/api/mailboxes/$MB_ID \\
  -H "Authorization: Bearer $KEY"
echo "✓ 邮箱已删除"`,
    },
    {
      title: '📈 10. 并发压测示例（wrk）',
      desc: '对注册接口进行高并发压测，500 并发，持续 30 秒',
      code: `# 安装 wrk: apt install wrk

# 导出注册脚本
cat > /tmp/register.lua << 'EOF'
wrk.method = "POST"
wrk.body   = '{"username": "user_' .. math.random(100000,999999) .. '"}'
wrk.headers["Content-Type"] = "application/json"
EOF

# 运行压测
wrk -t 10 -c 500 -d 30s --script /tmp/register.lua \\
  ${base}/public/register

# 或使用 k6
cat > /tmp/test.js << 'EOF'
import http from 'k6/http';
import { check } from 'k6';
export const options = { vus: 500, duration: '30s' };
const KEY = '${key}';
export default function() {
  const r = http.post(
    '${base}/api/mailboxes',
    '{}',
    { headers: { 'Authorization': 'Bearer ' + KEY, 'Content-Type': 'application/json' }}
  );
  check(r, { '创建成功': r => r.status === 201 });
}
EOF
k6 run /tmp/test.js`,
    },
  ];

  container.innerHTML = `
    <div style="max-width:860px">
      <div style="margin-bottom:1.2rem;padding:0.8rem 1rem;background:var(--bg-secondary);border-radius:8px;font-size:0.82rem">
        🔑 当前 API Key：
        <code style="margin-left:0.5rem;filter:blur(3px);cursor:pointer" onclick="this.style.filter='none'">${escHtml(key)}</code>
        <button class="copy-btn" onclick="copyText('${escHtml(key)}')" title="复制">⎘</button>
      </div>
      ${sections.map((s,i) => `
        <div class="card" style="margin-bottom:1rem">
          <div class="card-header"><div class="card-title">${escHtml(s.title)}</div></div>
          <div class="card-body">
            <p style="font-size:0.82rem;color:var(--text-secondary);margin-bottom:0.6rem">${escHtml(s.desc)}</p>
            <div class="code-box" style="white-space:pre;overflow-x:auto;font-size:0.75rem;line-height:1.6;position:relative">
              <button class="copy-btn" style="position:absolute;top:6px;right:6px" onclick="copyText(${JSON.stringify(s.code)})" title="复制">⎘</button>
              ${escHtml(s.code)}
            </div>
          </div>
        </div>
      `).join('')}
    </div>
  `;
}

// ─── 启动 ──────────────────────────────────────────────────
function init() {
  applyTheme(state.theme);

  if (state.apiKey && state.account) {
    showMainLayout();
    navigate('dashboard');
  } else if (state.apiKey) {
    // 验证 key
    tryLogin(state.apiKey);
  } else {
    showAuthPage();
  }
}

document.addEventListener('DOMContentLoaded', init);
