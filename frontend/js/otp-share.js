'use strict';

const shareAPIKey = location.pathname.split('/').filter(Boolean).pop() || '';
const shareNamespace = location.pathname.startsWith('/external-otp-share/') ? 'external-otp' : 'otp-share';
const shareBase = `/public/${shareNamespace}/page/${encodeURIComponent(decodeURIComponent(shareAPIKey))}`;
const $share = id => document.getElementById(id);

function shareEscape(value) {
  return String(value ?? '').replace(/[&<>"']/g, char => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  }[char]));
}

function shareFormatTime(value) {
  if (!value) return '永久有效';
  return new Date(value).toLocaleString('zh-CN', { hour12: false });
}

function shareToast(message, type = 'error') {
  const toast = document.createElement('div');
  toast.className = `toast ${type}`;
  toast.textContent = message;
  $share('toast-container')?.appendChild(toast);
  setTimeout(() => toast.remove(), 4000);
}

async function shareCopyText(text) {
  const value = String(text || '');
  if (!value) return false;
  let textarea = null;
  try {
    if (window.isSecureContext && navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(value);
    } else {
      textarea = document.createElement('textarea');
      textarea.value = value;
      textarea.setAttribute('readonly', '');
      textarea.style.position = 'fixed';
      textarea.style.opacity = '0';
      document.body.appendChild(textarea);
      textarea.focus();
      textarea.select();
      if (!document.execCommand('copy')) throw new Error('copy command failed');
    }
    return true;
  } catch (_) {
    return false;
  } finally {
    textarea?.remove();
  }
}

async function shareFetch(path) {
  const response = await fetch(path, { headers: { Accept: 'application/json' } });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`);
  return data;
}

async function renderSharedOTP(otp) {
  $share('share-otp-result').textContent = otp.code || '未找到';
  $share('share-otp-result').classList.add('is-ready');
  $share('share-otp-meta').textContent = `${otp.subject || '(无主题)'} · ${shareFormatTime(otp.received_at)}`;
  if (otp.code) {
    const copied = await shareCopyText(otp.code);
    shareToast(copied ? `OTP 已复制：${otp.code}` : 'OTP 已提取，请手动复制', copied ? 'success' : 'warn');
  }
}

function renderSharedEmails(emails) {
  const list = $share('share-mail-list');
  if (!emails.length) {
    list.innerHTML = '<div class="otp-share-empty">该邮箱暂时没有邮件。</div>';
    return;
  }
  list.innerHTML = emails.map(email => `
    <button class="otp-public-mail-item" type="button" data-email-id="${shareEscape(email.id)}">
      <strong>${shareEscape(email.subject || '(无主题)')}</strong>
      <span>${shareEscape(email.sender || '(未知发件人)')}</span>
      <time>${shareEscape(shareFormatTime(email.received_at))}</time>
    </button>
  `).join('');
  list.querySelectorAll('[data-email-id]').forEach(button => {
    button.addEventListener('click', () => loadSharedEmail(button.dataset.emailId, button));
  });
}

async function loadSharedEmail(emailID, button) {
  try {
    document.querySelectorAll('.otp-public-mail-item').forEach(item => item.classList.remove('active'));
    button?.classList.add('active');
    const data = await shareFetch(`${shareBase}/emails/${encodeURIComponent(emailID)}`);
    const email = data.email || {};
    const detail = $share('share-mail-detail');
    const attachments = (email.attachments || []).map(attachment => `
      <a class="btn btn-ghost btn-sm" href="${shareBase}/emails/${encodeURIComponent(emailID)}/attachments/${attachment.id}">
        下载 ${shareEscape(attachment.filename || '附件')}
      </a>
    `).join('');
    detail.className = 'otp-public-detail';
    detail.innerHTML = `
      <div class="email-detail-header">
        <div class="email-subject-big">${shareEscape(email.subject || '(无主题)')}</div>
        <div class="email-info-row"><strong>发件人：</strong>${shareEscape(email.sender || '—')}</div>
        <div class="email-info-row"><strong>收件人：</strong>${shareEscape(email.recipient || '—')}</div>
        <div class="email-info-row"><strong>时间：</strong>${shareEscape(shareFormatTime(email.received_at))}</div>
        <button class="btn btn-primary btn-sm" id="share-email-otp-btn">提取本封 OTP</button>
      </div>
      ${attachments ? `<div class="email-attachments"><div class="email-attachments-title">附件</div><div class="otp-public-attachments">${attachments}</div></div>` : ''}
      <div id="share-email-body"></div>
    `;
    const body = $share('share-email-body');
    if (email.body_html) {
      const frame = document.createElement('iframe');
      frame.className = 'email-body-frame otp-public-frame';
      frame.setAttribute('sandbox', 'allow-same-origin allow-popups');
      body.appendChild(frame);
      frame.contentDocument.open();
      frame.contentDocument.write(email.body_html);
      frame.contentDocument.close();
    } else {
      body.innerHTML = `<pre class="otp-public-text-body">${shareEscape(email.body_text || '(邮件内容为空)')}</pre>`;
    }
    $share('share-email-otp-btn').addEventListener('click', async () => {
      try {
        const otpData = await shareFetch(`${shareBase}/emails/${encodeURIComponent(emailID)}/otp`);
        await renderSharedOTP(otpData.otp || {});
        window.scrollTo({ top: 0, behavior: 'smooth' });
      } catch (error) {
        shareToast(error.message, 'warn');
      }
    });
  } catch (error) {
    shareToast(error.message);
  }
}

async function loadSharedMailbox() {
  const data = await shareFetch(`${shareBase}/mailbox`);
  $share('share-mailbox').textContent = data.mailbox?.full_address || '未知邮箱';
  $share('share-expiry').textContent = data.expires_at ? `有效至 ${shareFormatTime(data.expires_at)}` : '永久有效';
}

async function loadSharedLatestOTP() {
  const data = await shareFetch(`${shareBase}/latest`);
  await renderSharedOTP(data.otp || {});
}

async function loadSharedEmails() {
  const data = await shareFetch(`${shareBase}/emails`);
  renderSharedEmails(data.emails || []);
}

$share('share-otp-btn').addEventListener('click', () => loadSharedLatestOTP().catch(error => shareToast(error.message, 'warn')));
$share('share-read-btn').addEventListener('click', () => loadSharedEmails().catch(error => shareToast(error.message)));

Promise.all([loadSharedMailbox(), loadSharedEmails()]).catch(error => {
  $share('share-mailbox').textContent = error.message;
  $share('share-mail-list').innerHTML = `<div class="otp-share-empty">${shareEscape(error.message)}</div>`;
});
