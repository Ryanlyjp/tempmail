'use strict';

const externalState = { accounts: [], shares: [], mailboxes: [], account: '', mailbox: '', page: 1, accountPage: 1, expanded: '', generation: 0, email: '', mobileView: 'mailboxes', listRequest: 0, detailRequest: 0 };
let externalPollBusy = false;
setInterval(async () => {
  if (externalPollBusy || state.page !== 'admin-external' || !state.account?.is_admin || document.querySelector('.modal-overlay')) return;
  externalPollBusy = true;
  try {
    const data = await externalAPI('/accounts');
    if (state.page !== 'admin-external' || document.querySelector('.modal-overlay')) return;
    const old = externalState.accounts.find(a => a.id === externalState.account);
    const current = (data.accounts || []).find(a => a.id === externalState.account);
    if (current && old && (current.last_sync !== old.last_sync || current.syncing !== old.syncing)) {
      const generation = externalState.generation;
      const mailboxes = (await externalAPI(`/accounts/${current.id}/mailboxes`)).mailboxes || [];
      if (generation !== externalState.generation || state.page !== 'admin-external' || current.id !== externalState.account || document.querySelector('.modal-overlay')) return;
      externalState.accounts = data.accounts || [];
      externalState.mailboxes = mailboxes;
      externalRenderMailboxes();
      await externalLoadEmails();
    }
  } catch (_) { /* Explicit actions still surface API errors. */ }
  finally { externalPollBusy = false; }
}, 5000);
const externalProviders = {
  outlook: ['Outlook / Hotmail / Live', 'outlook.office365.com', '注册 Microsoft Entra 应用，选择允许的账号类型；为 Graph 授权 Mail.Read，或为 IMAP 授权 IMAP.AccessAsUser.All，并请求 offline_access。\n通过应用的 OAuth 授权流程取得 Client ID 和 Refresh Token；二者必须属于同一应用。个人账号可选 consumers，混合账号使用 common。OAuth IMAP 还需在 Outlook 网页设置中允许 IMAP。', 'https://learn.microsoft.com/en-us/exchange/client-developer/legacy-protocols/how-to-authenticate-an-imap-pop-smtp-application-by-using-oauth'],
  gmail: ['Gmail', 'imap.gmail.com', '先启用 Google 两步验证，再点击下方“创建应用专用密码”直达页面，创建名为 TempMail 的应用密码。填写完整 Gmail 地址和该密码，不是网页登录密码。\n个人 Gmail 已默认开启 IMAP。组织账号、安全密钥专用验证或高级保护可能限制应用密码；入口不可用时请检查账号安全策略。Promotions 分类不触发 TG 提醒。', 'https://support.google.com/accounts/answer/185833?hl=zh-Hans'],
  qq: ['QQ / Foxmail', 'imap.qq.com', '在 QQ 邮箱网页版的设置中找到 POP3/IMAP/SMTP 服务，开启 IMAP 并按页面要求验证身份、生成授权码。使用完整邮箱地址和授权码。\n不同版本的菜单位置可能不同，以邮箱当前页面为准。', 'https://service.mail.qq.com/'],
  '163': ['163 邮箱', 'imap.163.com', '网易邮箱网页版 → 设置 → POP3/SMTP/IMAP → 开启 IMAP，完成手机验证后取得客户端授权码。也可点击新增授权密码。授权码只在生成时显示，请及时保存。', 'https://help.mail.163.com/faq.do?m=list&categoryID=90'],
  '126': ['126 邮箱', 'imap.126.com', '126 邮箱网页版 → 设置 → POP3/SMTP/IMAP → 开启 IMAP，完成验证后取得客户端授权码。使用完整邮箱地址及授权码。', 'https://help.mail.163.com/faq.do?m=list&categoryID=90'],
  yahoo: ['Yahoo', 'imap.mail.yahoo.com', 'Yahoo Account Security → External connections → Create app password。填写完整邮箱地址及生成的应用密码。\nYahoo Japan 是独立服务，请使用自定义 IMAP 和其官方参数。', 'https://help.yahoo.com/kb/SLN15241.html'],
  aliyun: ['阿里个人邮箱', 'imap.aliyun.com', '在阿里个人邮箱网页设置中开启 IMAP／第三方客户端服务，按账号页面获取客户端密码。个人版与企业版服务器不同，请先核对邮箱提供的参数。', 'https://mail.aliyun.com/'],
  aliyun_enterprise: ['阿里企业邮箱', 'imap.qiye.aliyun.com', '由企业管理员开放 IMAP 权限，再在账号安全设置中取得客户端密码。默认国内服务地址 imap.qiye.aliyun.com；香港或其他区域请按官方文档修改服务器。', 'https://help.aliyun.com/document_detail/36576.html'],
  custom: ['自定义 IMAP', '', '向邮箱服务商获取 IMAP 服务器、TLS 端口及客户端授权凭据。默认端口 993，证书需要有效。仅支持加密连接。', ''],
};
const externalAPI = (path, method = 'GET', data) => apiFetch('/api/admin/external' + path, { method, ...(data === undefined ? {} : { body: JSON.stringify(data) }) });
const externalCatch = fn => async (...args) => { try { await fn(...args); } catch (e) { toast(e.message, 'error'); } };
const externalScope = () => externalState.mailbox ? `/mailboxes/${externalState.mailbox}` : `/accounts/${externalState.account}`;

async function renderExternalMail(container) {
  if (!state.account?.is_admin) { container.textContent = '仅管理员可访问'; return; }
  const generation = ++externalState.generation;
  const [accountData, shareData, settings] = await Promise.all([externalAPI('/accounts'), externalAPI('/shares'), api.publicSettings().catch(() => ({}))]);
  if (generation !== externalState.generation || state.page !== 'admin-external') return;
  externalState.accounts = accountData.accounts || [];
  externalState.shares = shareData.shares || [];
  applyPublicSettings(settings);
  if (!externalState.accounts.some(a => a.id === externalState.account)) {
    externalState.account = externalState.accounts[0]?.id || ''; externalState.mailbox = '';
  }
  const a = externalState.accounts.find(item => item.id === externalState.account);
  externalState.mailboxes = a ? (await externalAPI(`/accounts/${a.id}/mailboxes`)).mailboxes || [] : [];
  if (generation !== externalState.generation || state.page !== 'admin-external') return;
  if (!externalState.mailboxes.some(b => b.id === externalState.mailbox)) externalState.mailbox = '';
  container.innerHTML = `<div class="three-pane-grid external-grid" data-mobile-view="${externalState.mobileView}">
    <section class="pane pane-mailboxes">
      <div class="pane-header"><span class="pane-title">邮箱</span><div class="pane-header-actions"><button class="btn btn-ghost btn-sm" id="external-manage-shares">分享管理</button><button class="btn btn-primary btn-sm" id="external-add">+ 新增</button></div></div>
      <div class="pane-scroll" id="external-mailboxes"></div>
      <div id="external-account-pagination"></div>
    </section>
    <section class="pane pane-emails">
      <div class="pane-header"><div class="pane-header-main">${externalBackButton('mailboxes', '返回邮箱')}<span class="pane-title" id="external-list-title">邮件</span></div><div class="pane-header-actions">${a ? `${externalState.mailbox ? '<button class="btn btn-danger btn-sm" id="external-delete-mailbox">删除邮箱</button>' : '<button class="btn btn-primary btn-sm" id="external-account-otp">提取 OTP</button>'}<button class="btn btn-ghost btn-sm" id="external-sync" title="同步并刷新邮件">↻ 刷新</button>` : ''}</div></div>
      <div class="external-toolbar external-mailbox-actions" id="external-mailbox-actions"></div>
      <div class="pane-scroll" id="external-emails"><div class="empty-state">请添加外部邮箱</div></div><div class="pane-pager" id="external-pagination"></div>
    </section>
    <section class="pane pane-email-view" id="external-detail"><div class="pane-header"><div class="pane-header-main">${externalBackButton('emails', '返回邮件')}<span class="pane-title">邮件正文</span></div></div><div class="pane-scroll"><div class="empty-state">请在中栏选择一封邮件</div></div></section>
  </div>`;
  $('external-add').onclick = () => externalAccountForm();
  $('external-manage-shares').onclick = () => navigate('admin-external-shares');
  externalRenderMailboxes();
  if (!a) return;
  $('external-sync').onclick = externalCatch(async () => { const r = await externalAPI(`/accounts/${a.id}/sync`, 'POST'); toast(r.message, 'success'); await externalLoadEmails(); });
  if ($('external-account-otp')) $('external-account-otp').onclick = externalCatch(async () => externalShowOTP(await externalAPI(`/accounts/${a.id}/otp/latest`)));
  if ($('external-delete-mailbox')) $('external-delete-mailbox').onclick = () => externalDeleteMailboxForm(externalState.mailboxes.find(b => b.id === externalState.mailbox));
  externalMailboxActions(); await externalLoadEmails();
  if (externalState.email) await externalEmailDetail(externalScope(), externalState.email, false);
}

function externalBackButton(view, label) {
  return `<button class="pane-back-btn" onclick="externalSetView('${view}')">← ${label}</button>`;
}

function externalSetView(view) {
  externalState.mobileView = view;
  const grid = document.querySelector('.external-grid');
  if (grid) grid.dataset.mobileView = view;
}

function externalRenderMailboxes() {
  const list = $('external-mailboxes');
  if (!list) return;
  const size = getMailboxPageSize();
  const pages = Math.max(1, Math.ceil(externalState.accounts.length / size));
  externalState.accountPage = Math.min(Math.max(1, externalState.accountPage), pages);
  const start = (externalState.accountPage - 1) * size;
  const rows = externalState.accounts.slice(start, start + size);
  list.innerHTML = rows.map(a => `<div class="mailbox-card external-account-card ${a.id === externalState.account ? 'is-selected' : ''}">
    <div class="external-account-head"><button class="external-account-address" data-account="${escHtml(a.id)}" title="${escHtml(a.address)}">${escHtml(a.address)}</button><button class="external-expand" data-expand="${escHtml(a.id)}" title="${externalState.expanded === a.id ? '收起子邮箱' : '展开子邮箱'}" aria-expanded="${externalState.expanded === a.id}">${externalState.expanded === a.id ? '⌃' : '⌄'}</button></div>
    <div class="mailbox-actions external-account-actions">
      <button class="btn btn-ghost btn-sm" data-add-child="${escHtml(a.id)}" title="添加子邮箱" aria-label="添加子邮箱">＋</button>
      <button class="btn btn-ghost btn-sm" data-account-otp="${escHtml(a.id)}" title="提取并复制 OTP" aria-label="提取并复制 OTP">🔢</button>
      <button class="btn btn-ghost btn-sm ${a.tg_enabled ? 'is-active' : ''}" data-account-tg="${escHtml(a.id)}" title="TG 提醒：${a.tg_enabled ? '开启' : '关闭'}" aria-label="TG 提醒：${a.tg_enabled ? '开启' : '关闭'}">✈</button>
      <button class="btn btn-ghost btn-sm" data-copy-account="${escHtml(a.id)}" title="复制地址" aria-label="复制地址">⎘</button>
      <button class="btn btn-danger btn-sm" data-delete-account="${escHtml(a.id)}" title="删除邮箱" aria-label="删除邮箱">✕</button>
    </div>
    ${externalState.expanded === a.id ? `<div class="external-child-list">${a.id === externalState.account ? externalState.mailboxes.map(b => `<button class="external-mailbox-select ${b.id === externalState.mailbox ? 'is-selected' : ''}" data-mailbox="${escHtml(b.id)}"><span>${escHtml(b.address)}</span><span class="external-muted">${b.count} 封</span></button>`).join('') || '<span class="external-muted external-child-empty">暂无子邮箱</span>' : '<span class="external-muted external-child-empty">加载中…</span>'}</div>` : ''}
    </div>`).join('') || '<div class="empty-state">添加外部邮箱后，首次同步最近 3 天收到的邮件。</div>';
  $('external-account-pagination').innerHTML = pages > 1 ? externalAccountPager(externalState.accountPage, pages) : '';
  externalBindAccountPager();
  list.querySelectorAll('[data-account]').forEach(button => button.onclick = externalCatch(async () => {
    externalState.account = button.dataset.account; externalState.mailbox = ''; externalState.email = ''; externalState.page = 1;
    externalState.mobileView = 'emails'; await renderExternalMail($('page-content'));
  }));
  list.querySelectorAll('[data-expand]').forEach(button => button.onclick = externalCatch(async () => {
    const id = button.dataset.expand;
    if (externalState.expanded === id) { externalState.expanded = ''; externalRenderMailboxes(); return; }
    externalState.expanded = id;
    if (externalState.account !== id) {
      externalState.account = id; externalState.mailbox = ''; externalState.email = ''; externalState.page = 1;
      await renderExternalMail($('page-content'));
    } else externalRenderMailboxes();
  }));
  list.querySelectorAll('[data-add-child]').forEach(button => button.onclick = () => externalChildForm(button.dataset.addChild));
  list.querySelectorAll('[data-account-otp]').forEach(button => button.onclick = externalCatch(async () => externalShowOTP(await externalAPI(`/accounts/${button.dataset.accountOtp}/otp/latest`))));
  list.querySelectorAll('[data-account-tg]').forEach(button => button.onclick = externalCatch(async () => {
    const account = externalState.accounts.find(a => a.id === button.dataset.accountTg);
    await externalAPI(`/accounts/${account.id}/tg`, 'PUT', { tg_enabled: !account.tg_enabled });
    account.tg_enabled = !account.tg_enabled; externalRenderMailboxes();
  }));
  list.querySelectorAll('[data-copy-account]').forEach(button => button.onclick = () => {
    const account = externalState.accounts.find(a => a.id === button.dataset.copyAccount);
    copyText(account.address);
  });
  list.querySelectorAll('[data-delete-account]').forEach(button => button.onclick = () => {
    const account = externalState.accounts.find(a => a.id === button.dataset.deleteAccount);
    externalDeleteAccountForm(account);
  });
  list.querySelectorAll('[data-mailbox]').forEach(button => button.onclick = externalCatch(async () => {
    externalState.mailbox = button.dataset.mailbox; externalState.email = ''; externalState.page = 1;
    externalState.mobileView = 'emails'; await renderExternalMail($('page-content'));
  }));
}

function externalAccountPager(page, pages) {
  return `<div class="pane-pager"><button class="btn btn-ghost btn-sm pager-btn" data-account-page="1" ${page <= 1 ? 'disabled' : ''} aria-label="首页">《</button><button class="btn btn-ghost btn-sm pager-btn" data-account-page="${page - 1}" ${page <= 1 ? 'disabled' : ''} aria-label="上一页">&lt;</button><button class="pane-pager-status pane-pager-jump" id="external-account-page-jump" title="点击输入页码直达">${page} / ${pages}</button><button class="btn btn-ghost btn-sm pager-btn" data-account-page="${page + 1}" ${page >= pages ? 'disabled' : ''} aria-label="下一页">&gt;</button><button class="btn btn-ghost btn-sm pager-btn" data-account-page="${pages}" ${page >= pages ? 'disabled' : ''} aria-label="末页">》</button></div>`;
}

async function externalChangeAccountPage(page) {
  const size = getMailboxPageSize();
  const pages = Math.max(1, Math.ceil(externalState.accounts.length / size));
  externalState.accountPage = Math.min(Math.max(1, Number(page) || 1), pages);
  const next = externalState.accounts[(externalState.accountPage - 1) * size];
  if (next) { externalState.account = next.id; externalState.mailbox = ''; externalState.email = ''; externalState.page = 1; externalState.expanded = ''; }
  await renderExternalMail($('page-content'));
}

function externalBindAccountPager() {
  const pager = $('external-account-pagination');
  if (!pager) return;
  pager.querySelectorAll('[data-account-page]').forEach(button => button.onclick = () => externalCatch(externalChangeAccountPage)(button.dataset.accountPage));
  if ($('external-account-page-jump')) $('external-account-page-jump').onclick = () => {
    const pages = Math.max(1, Math.ceil(externalState.accounts.length / getMailboxPageSize()));
    showModal('跳转页码', `<label>页码（1-${pages}）<input class="form-input" id="external-account-page-input" type="number" min="1" max="${pages}" value="${externalState.accountPage}"></label>`, async () => {
      const page = Math.floor(Number($('external-account-page-input').value));
      if (!Number.isFinite(page) || page < 1 || page > pages) { toast(`请输入 1 到 ${pages} 之间的页码`, 'warn'); return false; }
      await externalChangeAccountPage(page); return true;
    });
  };
}

function externalChildForm(accountID) {
  const account = externalState.accounts.find(a => a.id === accountID);
  showModal('添加子邮箱', `<label>完整原收件邮箱<input class="form-input" id="external-child-address" type="email" placeholder="name@example.com"></label><p class="external-muted">只有归属明确的邮件才进入该子邮箱，不会仅因添加地址就授予其他邮件的访问权。</p>`, async () => {
    try {
      await externalAPI(`/accounts/${accountID}/mailboxes`, 'POST', { address: $('external-child-address').value });
      externalState.account = accountID; externalState.expanded = accountID; externalState.mailbox = ''; externalState.email = ''; externalState.page = 1;
      await renderExternalMail($('page-content')); toast(`已添加到 ${account.address}`, 'success'); return true;
    } catch (e) { toast(e.message, 'error'); return false; }
  });
}

function externalDeleteAccountForm(account) {
  showModal('删除外部母邮箱', `<p>确定删除母邮箱 <strong>${escHtml(account.address)}</strong>？</p><p style="font-size:.8rem;color:var(--clr-danger)">将永久删除整个母邮箱账号、该母邮箱同步的全部邮件、其下所有子邮箱及所有对应分享/API Key。</p>`, async () => {
    try {
      await externalAPI(`/accounts/${account.id}`, 'DELETE');
      if (externalState.account === account.id) { externalState.account = ''; externalState.mailbox = ''; externalState.email = ''; externalState.page = 1; externalState.expanded = ''; }
      await renderExternalMail($('page-content')); toast('外部邮箱已删除', 'success'); return true;
    } catch (e) { toast('删除失败：' + e.message, 'error'); return false; }
  });
  $('modal-confirm-btn').textContent = '确认删除';
  $('modal-confirm-btn').classList.add('btn-danger');
}

function externalDeleteMailboxForm(mailbox) {
  if (!mailbox) return;
  showModal('删除外部子邮箱', `<p>确定删除子邮箱 <strong>${escHtml(mailbox.address)}</strong>？</p><p style="font-size:.8rem;color:var(--clr-danger)">将删除该子邮箱记录、TG 设置及对应分享/API Key，不删除母邮箱已同步的原始邮件。以后收到发往该地址的新邮件时，子邮箱会被自动重新发现。</p>`, async () => {
    try {
      await externalAPI(`/mailboxes/${mailbox.id}`, 'DELETE');
      externalState.mailbox = ''; externalState.email = ''; externalState.page = 1;
      await renderExternalMail($('page-content')); toast('子邮箱记录已删除', 'success'); return true;
    } catch (e) { toast('删除失败：' + e.message, 'error'); return false; }
  });
  $('modal-confirm-btn').textContent = '确认删除';
  $('modal-confirm-btn').classList.add('btn-danger');
}

async function renderExternalShares(container) {
  if (!state.account?.is_admin) { container.textContent = '仅管理员可访问'; return; }
  const data = await externalAPI('/shares');
  if (state.page !== 'admin-external-shares') return;
  externalState.shares = data.shares || [];
  container.innerHTML = `<div class="external-toolbar"><button class="btn btn-ghost btn-sm" onclick="navigate('admin-external')">← 返回外部邮箱</button></div><section class="card external-card"><h3>已有分享</h3><p class="external-muted">选择邮箱后可创建分享。每个 Key 仅授权对应子邮箱，支持查看、有效期、更换、停止及收回。</p><div id="external-shares"></div></section>`;
  externalRenderShares();
}

function externalMailboxActions() {
  const b = externalState.mailboxes.find(item => item.id === externalState.mailbox);
  $('external-list-title').textContent = b?.address || '全部收到的邮件';
  $('external-mailbox-actions').innerHTML = b ? `<button class="btn btn-primary btn-sm" id="external-otp">提取并复制 OTP</button><button class="btn btn-ghost btn-sm" id="external-tg">TG 提醒：${b.tg_enabled ? '开启' : '关闭'}</button><button class="btn btn-ghost btn-sm" id="external-share">分享 / API</button><button class="external-id-copy" id="external-mailbox-id" title="复制子邮箱 ID">子邮箱 ID：${escHtml(b.id)}</button>` : '<span class="external-muted">未能确认原收件人的邮件仅显示在此处，不对外分享。</span>';
  if (!b) return;
  $('external-otp').onclick = externalCatch(async () => externalShowOTP(await externalAPI(`/mailboxes/${b.id}/otp/latest`)));
  $('external-tg').onclick = externalCatch(async () => { await externalAPI(`/mailboxes/${b.id}`, 'PUT', { tg_enabled: !b.tg_enabled }); b.tg_enabled = !b.tg_enabled; externalMailboxActions(); });
  $('external-share').onclick = () => externalShareForm(b);
  $('external-mailbox-id').onclick = () => copyText(b.id);
}

async function externalLoadEmails() {
  const scope = externalScope(); const page = externalState.page;
  const request = ++externalState.listRequest;
  const data = await externalAPI(`${scope}/emails?page=${page}`);
  if (request !== externalState.listRequest || state.page !== 'admin-external' || scope !== externalScope() || page !== externalState.page || !$('external-emails')) return;
  const pages = Math.max(1, Math.ceil(data.total / data.size));
  if (page > pages) { externalState.page = pages; return externalLoadEmails(); }
  $('external-emails').innerHTML = (data.emails || []).map(e => `<button class="email-item external-email-item ${e.id === externalState.email ? 'is-selected' : ''}" data-email="${escHtml(e.id)}"><span class="email-avatar">${escHtml((e.sender || '?').slice(0, 2).toUpperCase())}</span><span class="email-meta"><span class="email-from">${escHtml(e.sender)}</span><span class="email-subject">${escHtml(e.subject || '(无主题)')}</span><span class="email-preview">${escHtml(e.recipient || '原收件人待确认')}${e.promotion ? ' · 促销（不自动提醒）' : ''}</span></span><span class="email-time">${escHtml(timeAgo(e.received_at))}</span></button>`).join('') || '<div class="empty-state">暂无邮件，请等待同步或检查账号状态。</div>';
  $('external-emails').querySelectorAll('[data-email]').forEach(button => button.onclick = externalCatch(() => externalEmailDetail(scope, button.dataset.email)));
  $('external-pagination').innerHTML = `<button class="btn btn-ghost btn-sm" id="external-prev" ${page <= 1 ? 'disabled' : ''}>上一页</button><span>${page} / ${pages} · ${data.total} 封</span><button class="btn btn-ghost btn-sm" id="external-next" ${page >= pages ? 'disabled' : ''}>下一页</button>`;
  $('external-prev').onclick = externalCatch(async () => { externalState.page--; await externalLoadEmails(); });
  $('external-next').onclick = externalCatch(async () => { externalState.page++; await externalLoadEmails(); });
}

async function externalShowOTP(data) { const code = data.otp?.code; if (code) { await copyText(code); toast(`OTP：${code}`, 'success'); } }

async function externalEmailDetail(scope, id, show = true) {
  externalState.email = id;
  const request = ++externalState.detailRequest;
  if (show) externalSetView('detail');
  const pane = $('external-detail');
  pane.innerHTML = `<div class="pane-header">${externalBackButton('emails', '返回邮件')}<span class="pane-title">邮件正文</span></div><div class="pane-scroll"><div class="empty-state">加载中…</div></div>`;
  document.querySelectorAll('[data-email]').forEach(button => button.classList.toggle('is-selected', button.dataset.email === id));
  const data = await externalAPI(`${scope}/emails/${id}`);
  if (request !== externalState.detailRequest || state.page !== 'admin-external' || scope !== externalScope() || externalState.email !== id || pane !== $('external-detail')) return;
  const e = data.email; const detail = pane;
  detail.innerHTML = `<div class="pane-header"><div class="pane-header-main">${externalBackButton('emails', '返回邮件')}<span class="pane-title" title="${escHtml(e.subject)}">${escHtml(e.subject || '(无主题)')}</span></div><button class="btn btn-ghost btn-sm" id="external-detail-otp">取码并复制</button></div><div class="pane-scroll"><div class="email-detail-header"><div class="email-info-row"><span>发件人：<strong>${escHtml(e.sender)}</strong></span>${buildEmailRecipientMeta(externalState.accounts.find(a => a.id === externalState.account)?.address, e.recipient || '原收件人待确认')}<span>${escHtml(formatDate(e.received_at))}</span></div></div><div id="external-attachments"></div><div id="external-body"></div></div>`;
  const attachments = e.attachments || [];
  $('external-attachments').innerHTML = buildEmailAttachments('', '', attachments);
  $('external-attachments').querySelectorAll('button').forEach((button, index) => {
    button.removeAttribute('onclick'); button.dataset.attachment = attachments[index].id;
  });
  $('external-detail-otp').onclick = externalCatch(async () => externalShowOTP(await externalAPI(`${scope}/emails/${id}/otp`)));
  detail.querySelectorAll('[data-attachment]').forEach(button => button.onclick = externalCatch(async () => {
    const attachment = e.attachments.find(a => String(a.id) === button.dataset.attachment);
    const response = await fetch(`/api/admin/external${scope}/emails/${id}/attachments/${attachment.id}`, { headers: { Authorization: `Bearer ${state.apiKey}` } });
    if (!response.ok) throw new Error('附件下载失败');
    triggerBlobDownload(await response.blob(), attachment.filename);
  }));
  if (e.body_html) {
    const frame = document.createElement('iframe'); frame.className = 'email-body-frame'; frame.title = '邮件正文';
    frame.setAttribute('sandbox', 'allow-same-origin allow-popups'); frame.referrerPolicy = 'no-referrer';
    $('external-body').appendChild(frame);
    frame.contentDocument.open(); frame.contentDocument.write(e.body_html); frame.contentDocument.close();
    const resize = () => { if (frame.isConnected && frame.contentDocument?.body) frame.style.height = frame.contentDocument.body.scrollHeight + 20 + 'px'; };
    frame.addEventListener('load', resize); setTimeout(resize, 300);
  } else { const pre = document.createElement('div'); pre.className = 'email-body-text'; pre.textContent = e.body_text || '(邮件内容为空)'; $('external-body').appendChild(pre); }
}

function externalAccountForm(a = {}) {
  const options = Object.entries(externalProviders).map(([key, p]) => `<option value="${key}" ${key === (a.provider || 'gmail') ? 'selected' : ''}>${p[0]}</option>`).join('');
  showModal(a.id ? '编辑外部邮箱' : '接入外部邮箱', `<div class="external-dialog"><div class="external-fields">
    <label>邮箱类型<select class="form-input" id="external-provider" ${a.id ? 'disabled' : ''}>${options}</select></label>
    <label>完整邮箱<input class="form-input" id="external-address" type="email" value="${escHtml(a.address || '')}" ${a.id ? 'readonly' : ''}></label>
    <label>IMAP 服务器<input class="form-input" id="external-host" value="${escHtml(a.host || externalProviders[a.provider || 'gmail'][1])}" ${a.id ? 'readonly' : ''}></label>
    <label>TLS 端口<input class="form-input" id="external-port" type="number" value="${a.port || 993}" ${a.id ? 'readonly' : ''}></label>
    <label class="external-wide" id="external-password-label">授权码 / 应用专用密码<input class="form-input" type="password" id="external-password" autocomplete="new-password" placeholder="${a.id ? '留空保留已有凭据' : '不是网页登录密码'}"></label>
    <label class="external-outlook">接入方式<select class="form-input" id="external-protocol"><option value="auto">自动（Graph 优先）</option><option value="graph">Graph</option><option value="imap_oauth">OAuth IMAP</option></select></label>
    <label class="external-outlook">账号类型<select class="form-input" id="external-authority"><option value="common">common（个人与组织）</option><option value="consumers">consumers（仅个人）</option></select></label>
    <label class="external-outlook external-wide">Client ID<input class="form-input" id="external-client" placeholder="${a.id ? '留空保留已有配置' : ''}"></label>
    <label class="external-outlook external-wide">Refresh Token<input class="form-input" type="password" id="external-token" autocomplete="new-password" placeholder="${a.id ? '留空保留已有凭据' : ''}"></label>
    <label class="external-wide"><span><input id="external-enabled" type="checkbox" ${a.enabled !== false ? 'checked' : ''}> 启用每 60 秒同步</span></label>
    </div><div class="external-help" id="external-help"></div><button class="btn btn-ghost btn-sm" id="external-test">测试连接</button><span class="external-muted" id="external-test-result"></span></div>`, async () => {
      try { const r = await externalAPI(a.id ? `/accounts/${a.id}` : '/accounts', a.id ? 'PUT' : 'POST', values()); externalState.account = r.account.id; externalState.mailbox = ''; externalState.email = ''; externalState.page = 1; await renderExternalMail($('page-content')); toast('接入配置已保存', 'success'); return true; } catch (e) { toast(e.message, 'error'); return false; }
    });
  $('external-protocol').value = a.protocol || 'auto'; $('external-authority').value = a.authority || 'common';
  function values() { return { id: a.id || '', provider: $('external-provider').value, address: $('external-address').value, host: $('external-host').value, port: Number($('external-port').value), protocol: $('external-protocol').value, authority: $('external-authority').value, enabled: $('external-enabled').checked, credentials: { password: $('external-password').value, client_id: $('external-client').value, refresh_token: $('external-token').value } }; }
  function help(changeHost) {
    const key = $('external-provider').value; const p = externalProviders[key];
    if (changeHost) $('external-host').value = p[1];
    $('external-help').textContent = p[2];
    if (key === 'gmail') {
      [['开启两步验证', 'https://myaccount.google.com/signinoptions/two-step-verification'], ['创建应用专用密码', 'https://myaccount.google.com/apppasswords']].forEach(([label, url]) => {
        const link = document.createElement('a'); link.href = url; link.target = '_blank'; link.rel = 'noopener noreferrer'; link.textContent = '\n' + label + '：' + url; $('external-help').appendChild(link);
      });
    }
    if (p[3]) { const link = document.createElement('a'); link.href = p[3]; link.target = '_blank'; link.rel = 'noopener noreferrer'; link.textContent = ' 官方帮助'; $('external-help').appendChild(link); }
    document.querySelectorAll('.external-outlook').forEach(e => e.style.display = key === 'outlook' ? 'grid' : 'none');
    $('external-password-label').style.display = key === 'outlook' ? 'none' : 'grid';
  }
  $('external-provider').onchange = () => help(true); help(false);
  $('external-test').onclick = async () => { const button = $('external-test'); const result = $('external-test-result'); button.disabled = true; result.textContent = ' 正在连接…'; try { const r = await externalAPI('/test', 'POST', values()); result.textContent = ' ' + r.message; } catch (e) { result.textContent = ' ' + e.message; } finally { button.disabled = false; } };
}

function externalRenderShares() {
  if (!$('external-shares')) return;
  $('external-shares').innerHTML = externalState.shares.map((s, i) => `<div class="external-share-row"><strong>${escHtml(s.address)}</strong><span class="external-muted">${!s.enabled ? '已停止' : s.expires_at && new Date(s.expires_at) <= new Date() ? '已过期' : '有效'} · ${s.expires_at ? escHtml(formatDate(s.expires_at)) : '永久'}</span><button class="btn btn-ghost btn-sm" data-share="${i}">查看</button></div>`).join('') || '<p class="external-muted">暂无分享。选择子邮箱后创建。</p>';
  $('external-shares').querySelectorAll('[data-share]').forEach(button => button.onclick = () => { const s = externalState.shares[Number(button.dataset.share)]; externalShareForm({ id: s.mailbox_id, address: s.address }); });
}

function externalShellQuote(value) { return "'" + String(value).replace(/'/g, "'\\''") + "'"; }
function externalShareForm(b) {
  const s = externalState.shares.find(item => item.mailbox_id === b.id);
  const key = s?.api_key || '';
  const rows = key ? [
    ['分享 API Key', key],
    ['独立分享页面', `${location.origin}/external-otp-share/${encodeURIComponent(key)}`],
    ['提取最新 OTP', `curl -fsSL -H ${externalShellQuote('Authorization: Bearer ' + key)} ${externalShellQuote(location.origin + '/public/external-otp/latest?format=text')}`],
    ['最近 5 封邮件', `curl -fsSL -H ${externalShellQuote('Authorization: Bearer ' + key)} ${externalShellQuote(location.origin + '/public/external-otp/emails')}`],
  ] : [];
  showModal('分享 / API · ' + b.address, `<div class="external-dialog"><p class="external-muted">仅授权该子邮箱。更换 Key 会立即使原链接和原 API Key 失效。</p><label><input type="checkbox" id="external-share-enabled" ${s?.enabled !== false ? 'checked' : ''}> 启用分享</label><label class="external-help">有效期（天，0 为永久；留空保留原期限）<input class="form-input" type="number" min="0" max="3650" id="external-share-days" value="${s ? '' : '0'}"></label><label><input type="checkbox" id="external-share-rotate"> 更换 Key</label>
  ${rows.map(([label, value], i) => `<label class="external-help">${label}<div class="external-copy-row"><input class="form-input" readonly value="${escHtml(value)}" id="external-copy-${i}"><button class="btn btn-ghost btn-sm" data-copy="${i}">复制</button></div></label>`).join('')}
  ${s ? '<button class="btn btn-danger btn-sm" id="external-share-revoke">删除 / 收回分享</button>' : ''}</div>`, async () => {
    try { const days = $('external-share-days').value.trim(); const body = { enabled: $('external-share-enabled').checked, rotate: $('external-share-rotate').checked }; if (days !== '') body.expires_days = Number(days); await externalAPI(`/mailboxes/${b.id}/share`, 'PUT', body); externalState.shares = (await externalAPI('/shares')).shares; externalRenderShares(); setTimeout(() => externalShareForm(b), 0); return true; } catch (e) { toast(e.message, 'error'); return false; }
  });
  document.querySelectorAll('[data-copy]').forEach(button => button.onclick = () => copyText($('external-copy-' + button.dataset.copy).value));
  if (s) $('external-share-revoke').onclick = externalCatch(async () => {
    if (!confirm('确定收回此分享？当前链接和 Key 将立即失效。')) return;
    await externalAPI(`/mailboxes/${b.id}/share`, 'DELETE'); document.querySelector('.modal-overlay')?.remove(); externalState.shares = (await externalAPI('/shares')).shares; externalRenderShares(); toast('分享已收回', 'success');
  });
}
