import {api} from '../api.js';
import {$, esc, icon, toast, pager, statusDot} from '../ui.js';

let page = 1, digestId = '', status = '';
let timer = null;
const PER_PAGE = 20;

export function stopRunsRefresh() {
  if (timer) { clearTimeout(timer); timer = null; }
}

export async function renderRuns(view) {
  stopRunsRefresh();
  view.innerHTML = `<div class="section-head"><h2>История</h2></div>
    <div class="filters">
      ${icon('filter')}
      <select id="f-digest"><option value="">Все дайджесты</option></select>
      <select id="f-status">
        <option value="">Все статусы</option>
        <option value="ok">готов</option>
        <option value="error">ошибка</option>
        <option value="empty">пусто</option>
        <option value="processing">в работе</option>
      </select>
      <span id="rtotal" class="muted"></span>
    </div>
    <div id="rlist" class="list"><p class="empty">Загрузка…</p></div>
    <div id="rpager"></div>`;
  try {
    const opts = await api('/api/digests/options');
    const sel = $('#f-digest');
    opts.forEach(o => {
      const op = document.createElement('option');
      op.value = o.id; op.textContent = o.name;
      sel.appendChild(op);
    });
    if (digestId && !opts.some(o => o.id === digestId)) digestId = '';
    sel.value = digestId;
    $('#f-status').value = status;
    sel.onchange = () => { digestId = sel.value; page = 1; loadRuns(view); };
    $('#f-status').onchange = () => { status = $('#f-status').value; page = 1; loadRuns(view); };
    await loadRuns(view);
  } catch (e) {
    $('#rlist').innerHTML = `<p class="err">${esc(e.message)}</p>`;
  }
}

async function loadRuns(view) {
  stopRunsRefresh();
  const q = new URLSearchParams({page: String(page), per_page: String(PER_PAGE)});
  if (digestId) q.set('digest_id', digestId);
  if (status) q.set('status', status);
  const j = await api('/api/runs?' + q);
  if (page > 1 && !j.items.length) { page = 1; return loadRuns(view); }
  $('#rtotal').textContent = j.total ? `всего: ${j.total}` : '';
  const box = $('#rlist');
  box.innerHTML = j.items.length ? '' : '<p class="empty">Ничего не найдено.</p>';
  j.items.forEach(r => box.appendChild(runRow(r)));
  $('#rpager').replaceChildren(pager(j.total, page, PER_PAGE, p => { page = p; loadRuns(view); }));
  // Auto-refresh while something is processing and the tab is still open.
  if (j.items.some(r => r.status === 'processing') && location.hash.startsWith('#/runs')) {
    timer = setTimeout(() => loadRuns(view), 10000);
  }
}

function runRow(r) {
  const el = document.createElement('div');
  el.className = 'item';
  const mail = r.mail_status === 'failed'
    ? `<span class="mail-fail" title="Письмо не отправлено: ${esc(r.mail_error || '')}">${icon('mail-x')}</span>`
    : '';
  const count = r.materials_count ? ` · ${r.materials_count} материалов` : '';
  el.innerHTML = `
    <div class="item-main">
      <div class="item-title"><strong>${esc(r.digest_name)}</strong> ${statusDot(r.status)} ${mail}</div>
      <div class="muted">${new Date(r.processed_at).toLocaleString()} · период ${new Date(r.period_from).toLocaleString()} — ${new Date(r.period_to).toLocaleString()}${count}</div>
      <div class="muted">Источники: ${(r.analyzed_sources || []).map(esc).join(', ') || '—'}</div>
      ${r.error ? `<div class="muted err">${esc(r.error)}</div>` : ''}
    </div>
    <div class="actions">
      <button class="text-btn" data-act="open">${icon('external')} Открыть</button>
    </div>`;
  el.querySelector('[data-act=open]').onclick = async () => {
    // Open synchronously in the click handler so popup blockers stay quiet,
    // then navigate once the signed link arrives.
    const win = window.open('about:blank', '_blank');
    try {
      const j = await api(`/api/runs/${r.id}/view-link`);
      win.location = j.url;
    } catch (e) {
      if (win) win.close();
      toast(e.message, 'error');
    }
  };
  return el;
}
