# Frontend Redesign Implementation Plan (Phase 3 of 3)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild the frontend in the approved minimalist style (light "variant C", dark slate "variant B"): ES modules, three-state theme toggle, SVG icon sprite, hairline list rows, toasts and confirm modals, «История» filters + pagination + auto-refresh, «Дайджесты» pagination, retention setting in UI.

**Architecture:** `app.js` splits into ES modules under `web/js/` (no bundler; `<script type="module">`). `index.html` carries an inline SVG sprite and a head script that applies the stored theme before first paint. `styles.css` is rewritten around design tokens defined for light in `:root` and overridden for dark via `prefers-color-scheme` guard + `[data-theme]`. Tasks 1–4 add modules without wiring; Task 5 atomically swaps `index.html`/`styles.css` and deletes the old `app.js`, so the UI never ships half-migrated.

**Tech Stack:** Vanilla JS (ES modules), CSS custom properties, Go `embed` (no changes needed — `//go:embed web/*` embeds subdirectories recursively).

**Spec:** `docs/superpowers/specs/2026-08-29-service-improvements-design.md` (section 4).

**Order:** Requires Phase 1 (view links, mail status) and Phase 2 (pagination API, options endpoint).

---

## File Structure

**Create:**
- `internal/handlers/web/js/api.js` — fetch wrapper, tokens, refresh flow
- `internal/handlers/web/js/theme.js` — three-state theme
- `internal/handlers/web/js/ui.js` — `$`/`$$`/`esc`, icons, toasts, confirm modal, pager, status dots
- `internal/handlers/web/js/views/digests.js` — digest list + modal + tag input
- `internal/handlers/web/js/views/runs.js` — history list, filters, auto-refresh
- `internal/handlers/web/js/views/settings.js` — settings, users, retention field
- `internal/handlers/web/js/app.js` — boot, router, auth screens

**Rewrite:**
- `internal/handlers/web/index.html` — sprite, skeleton, module script
- `internal/handlers/web/styles.css` — token-based minimalist styles

**Delete:**
- `internal/handlers/web/app.js` (in Task 5, together with the swap)

There is no JS test runner in this project; JS tasks are verified by `go build` (embed) plus the manual smoke checklist in Task 6. Steps within tasks are therefore create → build → commit.

---

## Task 1: Core modules — api.js, theme.js, ui.js

**Files:**
- Create: `internal/handlers/web/js/api.js`, `internal/handlers/web/js/theme.js`, `internal/handlers/web/js/ui.js`

- [ ] **Step 1: Create `internal/handlers/web/js/api.js`**

```js
export const state = {
  access: localStorage.getItem('access') || '',
  refresh: localStorage.getItem('refresh') || '',
  user: null,
};

export function setTokens(access, refresh) {
  state.access = access; state.refresh = refresh;
  if (access) localStorage.setItem('access', access); else localStorage.removeItem('access');
  if (refresh) localStorage.setItem('refresh', refresh); else localStorage.removeItem('refresh');
}

export async function api(path, opts = {}) {
  opts.headers = Object.assign({'Content-Type': 'application/json'}, opts.headers || {});
  if (state.access) opts.headers['Authorization'] = 'Bearer ' + state.access;
  const res = await fetch(path, opts);
  if (res.status === 401 && state.refresh && !opts._retry) {
    const r = await fetch('/api/auth/refresh', {
      method: 'POST', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({refresh: state.refresh}),
    });
    if (r.ok) {
      const j = await r.json();
      setTokens(j.access, j.refresh);
      opts._retry = true;
      return api(path, opts);
    }
    setTokens('', ''); state.user = null;
    window.dispatchEvent(new Event('auth:logout'));
    throw new Error('unauthorized');
  }
  if (!res.ok) {
    let msg = res.statusText;
    try { const j = await res.json(); msg = j.error || msg; } catch {}
    throw new Error(msg);
  }
  if (res.status === 204) return null;
  return res.json();
}
```

- [ ] **Step 2: Create `internal/handlers/web/js/theme.js`**

```js
const ORDER = ['system', 'light', 'dark'];
const ICONS = {system: '#i-monitor', light: '#i-sun', dark: '#i-moon'};
const TITLES = {system: 'Тема: системная', light: 'Тема: светлая', dark: 'Тема: тёмная'};

export function currentTheme() {
  try {
    const t = localStorage.getItem('theme');
    if (ORDER.includes(t)) return t;
  } catch {}
  return 'system';
}

export function applyTheme(t) {
  if (t === 'light' || t === 'dark') document.documentElement.dataset.theme = t;
  else delete document.documentElement.dataset.theme;
  const use = document.getElementById('theme-icon');
  if (use) use.setAttribute('href', ICONS[t]);
  const btn = document.getElementById('theme-toggle');
  if (btn) btn.title = TITLES[t];
}

export function initTheme() {
  applyTheme(currentTheme());
  document.getElementById('theme-toggle').addEventListener('click', () => {
    const next = ORDER[(ORDER.indexOf(currentTheme()) + 1) % ORDER.length];
    try {
      if (next === 'system') localStorage.removeItem('theme');
      else localStorage.setItem('theme', next);
    } catch {}
    applyTheme(next);
  });
}
```

- [ ] **Step 3: Create `internal/handlers/web/js/ui.js`**

```js
export function $(sel, root = document) { return root.querySelector(sel); }
export function $$(sel, root = document) { return [...root.querySelectorAll(sel)]; }

export function esc(s) {
  return String(s == null ? '' : s).replace(/[&<>"']/g,
    c => ({'&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'})[c]);
}

export function icon(name, cls = '') {
  return `<svg class="icon${cls ? ' ' + cls : ''}" aria-hidden="true"><use href="#i-${name}"/></svg>`;
}

const RUN_STATUS = {
  ok: {label: 'готов', cls: 'ok'},
  error: {label: 'ошибка', cls: 'err'},
  empty: {label: 'пусто', cls: 'off'},
  processing: {label: 'в работе', cls: 'warn spin'},
};

export function statusDot(status) {
  const s = RUN_STATUS[status] || {label: status, cls: 'off'};
  return `<span class="dot ${s.cls}">● ${esc(s.label)}</span>`;
}

export function toast(msg, type = 'info') {
  const box = $('#toasts');
  const el = document.createElement('div');
  el.className = 'toast' + (type === 'error' ? ' error' : '');
  el.textContent = msg;
  el.onclick = () => el.remove();
  box.appendChild(el);
  setTimeout(() => el.remove(), 4000);
}

export function confirmDialog({title = 'Подтверждение', text = '', okLabel = 'Удалить'}) {
  return new Promise(resolve => {
    const modal = document.createElement('div');
    modal.className = 'modal';
    modal.innerHTML = `<div class="card">
      <h2>${esc(title)}</h2>
      <p class="muted">${esc(text)}</p>
      <div class="row actions-end">
        <button type="button" class="secondary" data-act="cancel">Отмена</button>
        <button type="button" class="danger" data-act="ok">${esc(okLabel)}</button>
      </div></div>`;
    const done = v => { modal.remove(); resolve(v); };
    modal.querySelector('[data-act=cancel]').onclick = () => done(false);
    modal.querySelector('[data-act=ok]').onclick = () => done(true);
    modal.addEventListener('click', e => { if (e.target === modal) done(false); });
    document.body.appendChild(modal);
  });
}

// pager renders «← 1 … 4 5 6 … N →» (±2 window). Hidden when total fits one page.
export function pager(total, page, perPage, onPage) {
  const el = document.createElement('div');
  el.className = 'pager';
  const pages = Math.max(1, Math.ceil(total / perPage));
  if (pages <= 1) return el;
  const add = (html, p, opts = {}) => {
    const b = document.createElement('button');
    b.className = 'page' + (opts.current ? ' current' : '') + (opts.dots ? ' dots' : '');
    b.innerHTML = html;
    b.disabled = !!opts.disabled || !!opts.current || !!opts.dots;
    if (!opts.dots && !opts.disabled && !opts.current) b.onclick = () => onPage(p);
    el.appendChild(b);
  };
  add(icon('chevron-left'), page - 1, {disabled: page <= 1});
  const win = [...new Set([1, pages, page - 2, page - 1, page, page + 1, page + 2]
    .filter(p => p >= 1 && p <= pages))].sort((a, b) => a - b);
  let prev = 0;
  win.forEach(p => {
    if (p - prev > 1) add('…', 0, {dots: true});
    add(String(p), p, {current: p === page});
    prev = p;
  });
  add(icon('chevron-right'), page + 1, {disabled: page >= pages});
  return el;
}
```

- [ ] **Step 4: Verify embed build**

Run: `go build ./...`
Expected: exit 0 (`//go:embed web/*` picks up the new `web/js/` directory automatically).

- [ ] **Step 5: Commit**

```bash
git add internal/handlers/web/js/
git commit -m "feat(ui): core frontend modules — api, theme, ui kit"
```

---

## Task 2: Digests view

**Files:**
- Create: `internal/handlers/web/js/views/digests.js`

- [ ] **Step 1: Create `internal/handlers/web/js/views/digests.js`**

```js
import {api} from '../api.js';
import {$, esc, icon, toast, confirmDialog, pager} from '../ui.js';

let page = 1;
const PER_PAGE = 20;

export async function renderDigests(view) {
  view.innerHTML = `<div class="section-head"><h2>Дайджесты</h2>
    <button class="primary" id="new-digest">${icon('plus')} Новый дайджест</button></div>
    <div id="dlist" class="list"><p class="empty">Загрузка…</p></div>
    <div id="dpager"></div>`;
  $('#new-digest').onclick = () => openDigestModal(null, view);
  try {
    const j = await api(`/api/digests?page=${page}&per_page=${PER_PAGE}`);
    if (page > 1 && !j.items.length) { page = 1; return renderDigests(view); }
    const box = $('#dlist');
    box.innerHTML = j.items.length ? '' : '<p class="empty">Пока ничего нет — создайте первый дайджест.</p>';
    j.items.forEach(d => box.appendChild(digestRow(d, view)));
    $('#dpager').replaceChildren(pager(j.total, page, PER_PAGE, p => { page = p; renderDigests(view); }));
  } catch (e) {
    $('#dlist').innerHTML = `<p class="err">${esc(e.message)}</p>`;
  }
}

function digestRow(d, view) {
  const el = document.createElement('div');
  el.className = 'item';
  const enabled = d.enabled
    ? '<span class="dot ok">● включен</span>'
    : '<span class="dot off">● выключен</span>';
  const kind = (d.kind || 'news') === 'facts' ? 'Факты' : 'Новости';
  el.innerHTML = `
    <div class="item-main">
      <div class="item-title"><strong>${esc(d.name)}</strong> ${enabled}
        <span class="muted">каждые ${d.frequency_hours} ч · ${kind} · ${esc(d.language)}</span></div>
      <div class="muted">${esc(d.topic || '')}</div>
      <div class="muted">Получатели: ${d.recipients.map(esc).join(', ') || '—'}</div>
      <div class="muted">Источники: ${(d.sources.length ? d.sources : d.auto_sources).map(esc).join(', ') || '— будут подобраны автоматически'}</div>
      <div class="muted">Последний запуск: ${d.last_run_at ? new Date(d.last_run_at).toLocaleString() : '—'} · Следующий: ${d.next_run_at ? new Date(d.next_run_at).toLocaleString() : '—'}</div>
    </div>
    <div class="actions">
      <button class="text-btn" data-act="run">${icon('play')} Запустить</button>
      <button class="text-btn" data-act="edit">${icon('edit')} Изменить</button>
      <button class="text-btn danger" data-act="del">${icon('trash')} Удалить</button>
    </div>`;
  el.querySelector('[data-act=run]').onclick = async () => {
    try {
      await api('/api/digests/' + d.id + '/run', {method: 'POST'});
      toast('Запуск поставлен в очередь');
    } catch (e) { toast(e.message, 'error'); }
  };
  el.querySelector('[data-act=edit]').onclick = () => openDigestModal(d, view);
  el.querySelector('[data-act=del]').onclick = async () => {
    const ok = await confirmDialog({
      title: 'Удалить дайджест?',
      text: `«${d.name}» и вся история его запусков будут удалены безвозвратно.`,
    });
    if (!ok) return;
    try {
      await api('/api/digests/' + d.id, {method: 'DELETE'});
      toast('Дайджест удалён');
      renderDigests(view);
    } catch (e) { toast(e.message, 'error'); }
  };
  return el;
}

function tagInput(initial, placeholder) {
  const wrap = document.createElement('div'); wrap.className = 'tag-input';
  const values = new Set(initial || []);
  const input = document.createElement('input'); input.placeholder = placeholder || '';
  function redraw() {
    wrap.innerHTML = '';
    values.forEach(v => {
      const t = document.createElement('span'); t.className = 'tag';
      t.innerHTML = esc(v) + ' <button type="button">×</button>';
      t.querySelector('button').onclick = () => { values.delete(v); redraw(); };
      wrap.appendChild(t);
    });
    wrap.appendChild(input); input.focus();
  }
  input.addEventListener('keydown', e => {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault();
      const v = input.value.trim();
      if (v) { values.add(v); input.value = ''; redraw(); }
    } else if (e.key === 'Backspace' && !input.value && values.size) {
      const last = [...values].pop(); values.delete(last); redraw();
    }
  });
  input.addEventListener('blur', () => {
    const v = input.value.trim();
    if (v) { values.add(v); input.value = ''; redraw(); }
  });
  redraw();
  return {el: wrap, get: () => [...values]};
}

function openDigestModal(d, view) {
  const editing = !!d;
  d = d || {name: '', topic: '', sources: [], ignored_sources: [], frequency_hours: 24, recipients: [], language: 'ru', kind: 'news', enabled: true};
  const modal = document.createElement('div'); modal.className = 'modal';
  modal.innerHTML = `<form class="card"><h2>${editing ? 'Редактирование' : 'Новый'} дайджест</h2>
    <label>Название<input name="name" required></label>
    <label>Тип дайджеста
      <select name="kind">
        <option value="news">Новостной дайджест</option>
        <option value="facts">Подборка интересных фактов</option>
      </select>
    </label>
    <label>Тематика (описание для анализа)<textarea name="topic"></textarea></label>
    <div class="row">
      <label>Регулярность, часов<input type="number" min="1" name="frequency_hours" required></label>
      <label>Язык<input name="language" required></label>
    </div>
    <label>Источники для анализа <div id="ta-sources"></div></label>
    <label>Игнорируемые источники <div id="ta-ignored"></div></label>
    <label>Получатели (email) <div id="ta-recipients"></div></label>
    <label class="check"><input type="checkbox" name="enabled"> Включен</label>
    <div class="err" id="d-err"></div>
    <div class="row actions-end">
      <button type="button" class="secondary" id="d-cancel">Отмена</button>
      <button type="submit" class="primary">Сохранить</button>
    </div></form>`;
  document.body.appendChild(modal);
  const form = modal.querySelector('form');
  form.name.value = d.name; form.topic.value = d.topic; form.frequency_hours.value = d.frequency_hours;
  form.language.value = d.language; form.enabled.checked = !!d.enabled;
  form.kind.value = d.kind || 'news';
  const ts = tagInput(d.sources, 'example.com, Enter'); modal.querySelector('#ta-sources').appendChild(ts.el);
  const ti = tagInput(d.ignored_sources, 'домен, Enter'); modal.querySelector('#ta-ignored').appendChild(ti.el);
  const tr = tagInput(d.recipients, 'mail@example.com, Enter'); modal.querySelector('#ta-recipients').appendChild(tr.el);
  modal.querySelector('#d-cancel').onclick = () => modal.remove();
  form.onsubmit = async (e) => {
    e.preventDefault();
    const payload = {
      name: form.name.value.trim(),
      topic: form.topic.value.trim(),
      kind: form.kind.value,
      frequency_hours: parseInt(form.frequency_hours.value, 10),
      language: form.language.value.trim(),
      enabled: form.enabled.checked,
      sources: ts.get(), ignored_sources: ti.get(), recipients: tr.get(),
    };
    try {
      if (editing) await api('/api/digests/' + d.id, {method: 'PUT', body: JSON.stringify(payload)});
      else await api('/api/digests', {method: 'POST', body: JSON.stringify(payload)});
      modal.remove();
      toast('Сохранено');
      renderDigests(view);
    } catch (err) { modal.querySelector('#d-err').textContent = err.message; }
  };
}
```

- [ ] **Step 2: Build and commit**

Run: `go build ./...`
Expected: exit 0.

```bash
git add internal/handlers/web/js/views/digests.js
git commit -m "feat(ui): digests view with pagination and confirm modal"
```

---

## Task 3: Runs (history) view

**Files:**
- Create: `internal/handlers/web/js/views/runs.js`

- [ ] **Step 1: Create `internal/handlers/web/js/views/runs.js`**

```js
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
```

- [ ] **Step 2: Build and commit**

Run: `go build ./...`
Expected: exit 0.

```bash
git add internal/handlers/web/js/views/runs.js
git commit -m "feat(ui): history view with filters, pagination and auto-refresh"
```

---

## Task 4: Settings view

**Files:**
- Create: `internal/handlers/web/js/views/settings.js`

- [ ] **Step 1: Create `internal/handlers/web/js/views/settings.js`**

```js
import {api} from '../api.js';
import {$, esc, icon, toast, confirmDialog} from '../ui.js';

export async function renderSettings(view) {
  view.innerHTML = `<div class="section-head"><h2>Настройки</h2></div>
    <div class="card"><h3>Обработка</h3>
      <div class="row">
        <label>Хранить историю, дней (0 — вечно)<input type="number" min="0" id="keep-days"></label>
      </div>
      <div class="row actions-end">
        <button id="toggle-proc" class="secondary">…</button>
        <button id="save-proc" class="primary">Сохранить</button>
      </div>
    </div>
    <div class="card"><h3>API и SMTP</h3>
      <form id="settings-form">
        <label>Cursor API key<input name="cursor_api_key" placeholder="оставьте пустым чтобы не менять"></label>
        <label>Cursor Repository URL<input name="cursor_repository" placeholder="https://github.com/user/repo"></label>
        <div class="row">
          <label>SMTP хост<input name="smtp_host"></label>
          <label>SMTP порт<input type="number" name="smtp_port"></label>
        </div>
        <div class="row">
          <label>SMTP пользователь<input name="smtp_user"></label>
          <label>SMTP пароль<input name="smtp_password" placeholder="оставьте пустым чтобы не менять"></label>
        </div>
        <div class="row">
          <label>From<input name="smtp_from"></label>
          <label class="check"><input type="checkbox" name="smtp_tls"> TLS (465)</label>
        </div>
        <div class="row actions-end"><button type="submit" class="primary">Сохранить</button></div>
      </form>
    </div>
    <div class="card"><h3>Пользователи</h3>
      <div id="ulist" class="list"><p class="empty">Загрузка…</p></div>
      <div class="row actions-end" style="margin-top:14px">
        <button id="new-user">${icon('plus')} Добавить пользователя</button>
      </div>
    </div>`;
  try {
    const s = await api('/api/settings');
    const form = $('#settings-form');
    $('#keep-days').value = s.keep_runs_days || 0;
    form.cursor_api_key.value = '';
    form.cursor_api_key.placeholder = s.cursor_api_key ? 'скрыто — оставьте пустым' : 'введите ключ';
    form.cursor_repository.value = s.cursor_repository || '';
    form.smtp_host.value = s.smtp_host || '';
    form.smtp_port.value = s.smtp_port || 587;
    form.smtp_user.value = s.smtp_user || '';
    form.smtp_password.value = '';
    form.smtp_password.placeholder = s.smtp_password ? 'скрыто' : '';
    form.smtp_from.value = s.smtp_from || '';
    form.smtp_tls.checked = !!s.smtp_tls;

    const btn = $('#toggle-proc');
    btn.textContent = s.processing_paused ? 'Возобновить обработку' : 'Приостановить обработку';
    btn.onclick = async () => {
      try {
        const cur = await api('/api/settings');
        cur.processing_paused = !cur.processing_paused;
        cur.cursor_api_key = ''; cur.smtp_password = '';
        await api('/api/settings', {method: 'PUT', body: JSON.stringify(cur)});
        toast(cur.processing_paused ? 'Обработка приостановлена' : 'Обработка возобновлена');
        renderSettings(view);
      } catch (e) { toast(e.message, 'error'); }
    };
    $('#save-proc').onclick = async () => {
      try {
        const cur = await api('/api/settings');
        cur.keep_runs_days = parseInt($('#keep-days').value, 10) || 0;
        cur.cursor_api_key = ''; cur.smtp_password = '';
        await api('/api/settings', {method: 'PUT', body: JSON.stringify(cur)});
        toast('Сохранено');
      } catch (e) { toast(e.message, 'error'); }
    };
    form.onsubmit = async (e) => {
      e.preventDefault();
      const payload = {
        cursor_api_key: form.cursor_api_key.value,
        cursor_repository: form.cursor_repository.value,
        smtp_host: form.smtp_host.value,
        smtp_port: parseInt(form.smtp_port.value, 10) || 587,
        smtp_user: form.smtp_user.value,
        smtp_password: form.smtp_password.value,
        smtp_from: form.smtp_from.value,
        smtp_tls: form.smtp_tls.checked,
        processing_paused: s.processing_paused,
        keep_runs_days: parseInt($('#keep-days').value, 10) || 0,
      };
      try {
        await api('/api/settings', {method: 'PUT', body: JSON.stringify(payload)});
        toast('Сохранено');
        renderSettings(view);
      } catch (err) { toast(err.message, 'error'); }
    };

    await renderUsers(view);
    $('#new-user').onclick = () => openUserModal(null, view);
  } catch (e) {
    view.insertAdjacentHTML('beforeend', `<p class="err">${esc(e.message)}</p>`);
  }
}

async function renderUsers(view) {
  const users = await api('/api/users');
  const box = $('#ulist');
  box.innerHTML = '';
  users.forEach(u => {
    const el = document.createElement('div');
    el.className = 'item';
    el.innerHTML = `<div class="item-main"><strong>${esc(u.email)}</strong>
        <div class="muted">${new Date(u.created_at).toLocaleString()}</div></div>
      <div class="actions">
        <button class="text-btn" data-act="edit">${icon('edit')} Изменить</button>
        <button class="text-btn danger" data-act="del">${icon('trash')} Удалить</button>
      </div>`;
    el.querySelector('[data-act=edit]').onclick = () => openUserModal(u, view);
    el.querySelector('[data-act=del]').onclick = async () => {
      const ok = await confirmDialog({title: 'Удалить пользователя?', text: u.email});
      if (!ok) return;
      try {
        await api('/api/users/' + u.id, {method: 'DELETE'});
        toast('Пользователь удалён');
        renderSettings(view);
      } catch (e) { toast(e.message, 'error'); }
    };
    box.appendChild(el);
  });
}

function openUserModal(u, view) {
  const editing = !!u;
  const modal = document.createElement('div'); modal.className = 'modal';
  modal.innerHTML = `<form class="card"><h2>${editing ? 'Изменить' : 'Новый'} пользователь</h2>
    <label>Email<input name="email" type="email" required></label>
    <label>Пароль ${editing ? '(пусто — не менять)' : ''}<input name="password" type="password" ${editing ? '' : 'required'}></label>
    <div class="err" id="u-err"></div>
    <div class="row actions-end">
      <button type="button" class="secondary" id="u-cancel">Отмена</button>
      <button type="submit" class="primary">Сохранить</button>
    </div></form>`;
  document.body.appendChild(modal);
  if (editing) modal.querySelector('[name=email]').value = u.email;
  modal.querySelector('#u-cancel').onclick = () => modal.remove();
  modal.querySelector('form').onsubmit = async (e) => {
    e.preventDefault();
    const f = e.target;
    const payload = {email: f.email.value.trim(), password: f.password.value};
    try {
      if (editing) await api('/api/users/' + u.id, {method: 'PUT', body: JSON.stringify(payload)});
      else await api('/api/users', {method: 'POST', body: JSON.stringify(payload)});
      modal.remove();
      toast('Сохранено');
      renderSettings(view);
    } catch (err) { modal.querySelector('#u-err').textContent = err.message; }
  };
}
```

- [ ] **Step 2: Build and commit**

Run: `go build ./...`
Expected: exit 0.

```bash
git add internal/handlers/web/js/views/settings.js
git commit -m "feat(ui): settings view with retention field and toasts"
```

---

## Task 5: Atomic swap — app.js, index.html, styles.css

**Files:**
- Create: `internal/handlers/web/js/app.js`
- Rewrite: `internal/handlers/web/index.html`, `internal/handlers/web/styles.css`
- Delete: `internal/handlers/web/app.js`

- [ ] **Step 1: Create `internal/handlers/web/js/app.js`**

```js
import {api, state, setTokens} from './api.js';
import {$, $$} from './ui.js';
import {initTheme} from './theme.js';
import {renderDigests} from './views/digests.js';
import {renderRuns, stopRunsRefresh} from './views/runs.js';
import {renderSettings} from './views/settings.js';

// ---------- Auth ----------

$('#login-form').addEventListener('submit', async (e) => {
  e.preventDefault();
  const fd = new FormData(e.target);
  try {
    const res = await fetch('/api/auth/login', {
      method: 'POST', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({email: fd.get('email'), password: fd.get('password')}),
    });
    const j = await res.json();
    if (j.error) throw new Error(j.error);
    setTokens(j.access, j.refresh);
    state.user = j.user;
    showApp();
  } catch (err) {
    $('#login-err').textContent = err.message;
  }
});

$('#logout').addEventListener('click', logout);
window.addEventListener('auth:logout', showLogin);

async function logout() {
  if (state.refresh) {
    try {
      await fetch('/api/auth/logout', {
        method: 'POST', headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({refresh: state.refresh}),
      });
    } catch {}
  }
  setTokens('', '');
  state.user = null;
  showLogin();
}

function showLogin() {
  stopRunsRefresh();
  $('#app').classList.add('hidden');
  $('#login').classList.remove('hidden');
}

function showApp() {
  $('#login').classList.add('hidden');
  $('#app').classList.remove('hidden');
  if (state.user) $('#user-email').textContent = state.user.email;
  route();
}

// ---------- Routing ----------

window.addEventListener('hashchange', route);

function route() {
  if (!state.access) { showLogin(); return; }
  stopRunsRefresh();
  const h = location.hash || '#/digests';
  $$('header nav a').forEach(a => a.classList.toggle('active', h.startsWith(a.getAttribute('href'))));
  const view = $('#view');
  if (h.startsWith('#/runs')) renderRuns(view);
  else if (h.startsWith('#/settings')) renderSettings(view);
  else renderDigests(view);
}

// ---------- Boot ----------

async function boot() {
  initTheme();
  if (!state.access) { showLogin(); return; }
  try {
    state.user = await api('/api/me');
    showApp();
  } catch {
    showLogin();
  }
}

boot();
```

- [ ] **Step 2: Rewrite `internal/handlers/web/index.html`**

Replace the entire file with:

```html
<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8">
<title>News Analyzer</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<script>
try {
  var t = localStorage.getItem('theme');
  if (t === 'light' || t === 'dark') document.documentElement.dataset.theme = t;
} catch (e) {}
</script>
<link rel="stylesheet" href="/styles.css">
</head>
<body>
<svg xmlns="http://www.w3.org/2000/svg" style="display:none" aria-hidden="true">
  <symbol id="i-digest" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></symbol>
  <symbol id="i-history" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 3"/></symbol>
  <symbol id="i-settings" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="4" y1="21" x2="4" y2="14"/><line x1="4" y1="10" x2="4" y2="3"/><line x1="12" y1="21" x2="12" y2="12"/><line x1="12" y1="8" x2="12" y2="3"/><line x1="20" y1="21" x2="20" y2="16"/><line x1="20" y1="12" x2="20" y2="3"/><line x1="1" y1="14" x2="7" y2="14"/><line x1="9" y1="8" x2="15" y2="8"/><line x1="17" y1="16" x2="23" y2="16"/></symbol>
  <symbol id="i-play" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="6 4 20 12 6 20 6 4"/></symbol>
  <symbol id="i-edit" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17 3a2.83 2.83 0 0 1 4 4L7.5 20.5 2 22l1.5-5.5Z"/></symbol>
  <symbol id="i-trash" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></symbol>
  <symbol id="i-plus" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></symbol>
  <symbol id="i-logout" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" y1="12" x2="9" y2="12"/></symbol>
  <symbol id="i-mail" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 4h16a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2z"/><polyline points="22,6 12,13 2,6"/></symbol>
  <symbol id="i-mail-x" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 4h16a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2z"/><polyline points="22,6 12,13 2,6"/><line x1="2" y1="2" x2="22" y2="22"/></symbol>
  <symbol id="i-filter" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="22 3 2 3 10 12.46 10 19 14 21 14 12.46 22 3"/></symbol>
  <symbol id="i-chevron-left" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="15 18 9 12 15 6"/></symbol>
  <symbol id="i-chevron-right" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="9 18 15 12 9 6"/></symbol>
  <symbol id="i-external" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></symbol>
  <symbol id="i-sun" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/></symbol>
  <symbol id="i-moon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></symbol>
  <symbol id="i-monitor" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="3" width="20" height="14" rx="2"/><line x1="8" y1="21" x2="16" y2="21"/><line x1="12" y1="17" x2="12" y2="21"/></symbol>
  <symbol id="i-check" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></symbol>
  <symbol id="i-x" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></symbol>
</svg>

<div id="login" class="login hidden">
  <form id="login-form" class="card">
    <h1>News Analyzer</h1>
    <label>Email <input type="email" name="email" required></label>
    <label>Пароль <input type="password" name="password" required></label>
    <button type="submit" class="primary" style="width:100%;justify-content:center">Войти</button>
    <p class="err" id="login-err"></p>
  </form>
</div>

<div id="app" class="app hidden">
  <header>
    <div class="brand">News Analyzer</div>
    <nav>
      <a href="#/digests"><svg class="icon" aria-hidden="true"><use href="#i-digest"/></svg>Дайджесты</a>
      <a href="#/runs"><svg class="icon" aria-hidden="true"><use href="#i-history"/></svg>История</a>
      <a href="#/settings"><svg class="icon" aria-hidden="true"><use href="#i-settings"/></svg>Настройки</a>
    </nav>
    <div class="user">
      <button id="theme-toggle" class="icon-btn" title="Тема"><svg class="icon" aria-hidden="true"><use id="theme-icon" href="#i-monitor"/></svg></button>
      <span id="user-email"></span>
      <button id="logout" class="icon-btn" title="Выход"><svg class="icon" aria-hidden="true"><use href="#i-logout"/></svg></button>
    </div>
  </header>
  <main id="view"></main>
</div>

<div id="toasts"></div>
<script type="module" src="/js/app.js"></script>
</body>
</html>
```

- [ ] **Step 3: Rewrite `internal/handlers/web/styles.css`**

Replace the entire file with:

```css
/* ---------- Tokens ---------- */
:root{
  --bg:#ffffff;
  --surface:#ffffff;
  --text:#171717;
  --muted:#737373;
  --line:#e5e5e5;
  --line-strong:#d4d4d4;
  --hover:#f5f5f5;
  --accent:#1d4ed8;
  --accent-contrast:#ffffff;
  --focus:rgba(29,78,216,.25);
  --ok:#15803d;
  --err:#b91c1c;
  --warn:#a16207;
  --overlay:rgba(23,23,23,.45);
  --shadow:0 4px 24px rgba(0,0,0,.12);
  --radius:8px;
}
@media (prefers-color-scheme: dark){
  :root:not([data-theme="light"]){
    --bg:#0f172a;
    --surface:#1e293b;
    --text:#f1f5f9;
    --muted:#94a3b8;
    --line:#1e293b;
    --line-strong:#334155;
    --hover:#1e293b;
    --accent:#93c5fd;
    --accent-contrast:#0f172a;
    --focus:rgba(147,197,253,.3);
    --ok:#4ade80;
    --err:#f87171;
    --warn:#fbbf24;
    --overlay:rgba(2,6,23,.6);
    --shadow:0 4px 24px rgba(0,0,0,.5);
  }
}
:root[data-theme="dark"]{
  --bg:#0f172a;
  --surface:#1e293b;
  --text:#f1f5f9;
  --muted:#94a3b8;
  --line:#1e293b;
  --line-strong:#334155;
  --hover:#1e293b;
  --accent:#93c5fd;
  --accent-contrast:#0f172a;
  --focus:rgba(147,197,253,.3);
  --ok:#4ade80;
  --err:#f87171;
  --warn:#fbbf24;
  --overlay:rgba(2,6,23,.6);
  --shadow:0 4px 24px rgba(0,0,0,.5);
}

/* ---------- Base ---------- */
*{box-sizing:border-box}
html,body{height:100%}
body{
  margin:0;
  font-family:-apple-system,"Segoe UI",Roboto,Inter,Arial,sans-serif;
  background:var(--bg);
  color:var(--text);
  font-size:14px;
  line-height:1.5;
}
.hidden{display:none !important}
a{color:var(--accent);text-decoration:none}
a:hover{text-decoration:underline}
.muted{color:var(--muted);font-size:13px;word-break:break-word}
.muted.err,.err{color:var(--err);font-size:13px}
p.err{min-height:18px}
.empty{color:var(--muted);padding:32px 0;text-align:center}
.icon{width:16px;height:16px;flex-shrink:0}

/* ---------- Buttons ---------- */
button{
  font:inherit;
  background:var(--surface);
  color:var(--text);
  border:1px solid var(--line-strong);
  padding:8px 14px;
  border-radius:var(--radius);
  cursor:pointer;
  display:inline-flex;
  align-items:center;
  gap:7px;
  transition:background .12s,color .12s,border-color .12s;
}
button:hover{background:var(--hover)}
button:disabled{opacity:.45;cursor:default}
button.primary{background:var(--accent);border-color:var(--accent);color:var(--accent-contrast);font-weight:600}
button.primary:hover{filter:brightness(1.08);background:var(--accent)}
button.danger{background:transparent;border-color:var(--err);color:var(--err)}
button.danger:hover{background:var(--err);color:#fff}
button.text-btn{border:0;background:none;padding:5px 8px;color:var(--muted);font-weight:500;border-radius:6px}
button.text-btn:hover{background:var(--hover);color:var(--text)}
button.text-btn.danger{color:var(--err)}
button.text-btn.danger:hover{background:var(--hover);color:var(--err)}
button.icon-btn{border:0;background:none;padding:7px;color:var(--muted);border-radius:6px}
button.icon-btn:hover{background:var(--hover);color:var(--text)}

/* ---------- Forms ---------- */
input,textarea,select{
  width:100%;
  padding:8px 12px;
  border:1px solid var(--line-strong);
  border-radius:var(--radius);
  font:inherit;
  font-size:14px;
  background:var(--surface);
  color:var(--text);
  transition:border-color .12s,box-shadow .12s;
}
input:focus,textarea:focus,select:focus{outline:0;border-color:var(--accent);box-shadow:0 0 0 3px var(--focus)}
textarea{min-height:96px;resize:vertical}
select{cursor:pointer}
label{display:block;margin-bottom:14px;font-size:13px;font-weight:500;color:var(--muted)}
label > input,label > textarea,label > select{margin-top:6px;color:var(--text);font-weight:400}
label.check{
  display:inline-flex;align-items:center;gap:8px;
  margin-bottom:0;font-weight:400;color:var(--text);cursor:pointer;user-select:none;
}
label.check input[type=checkbox]{width:auto;margin:0;accent-color:var(--accent);cursor:pointer}

/* ---------- Login ---------- */
.login{min-height:100vh;display:flex;align-items:center;justify-content:center;padding:24px;background:var(--bg)}
.login .card{width:100%;max-width:360px}
.login h1{margin:0 0 20px;font-size:20px}

/* ---------- Header ---------- */
header{
  display:flex;align-items:stretch;gap:24px;
  padding:0 28px;
  height:56px;
  background:var(--bg);
  border-bottom:1px solid var(--line);
  position:sticky;top:0;z-index:5;
}
header .brand{font-weight:700;font-size:15px;letter-spacing:.2px;align-self:center}
header nav{display:flex;gap:4px;flex:1;margin-left:8px}
header nav a{
  display:inline-flex;align-items:center;gap:7px;
  padding:0 12px;
  color:var(--muted);font-weight:500;
  border-bottom:2px solid transparent;
}
header nav a:hover{color:var(--text);text-decoration:none}
header nav a.active{color:var(--text);border-bottom-color:var(--accent)}
header .user{display:flex;align-items:center;gap:8px;font-size:13px;color:var(--muted)}
header .user #user-email{font-weight:500;color:var(--text)}

/* ---------- Layout ---------- */
main{padding:28px;max-width:1000px;margin:0 auto}
.section-head{display:flex;align-items:center;justify-content:space-between;margin-bottom:8px;gap:16px;flex-wrap:wrap}
.section-head h2{margin:0;font-size:20px;font-weight:650}

/* ---------- Lists / items ---------- */
.list{display:flex;flex-direction:column}
.list > .item:first-child{border-top:1px solid var(--line)}
.item{
  border-bottom:1px solid var(--line);
  padding:16px 0;
  display:flex;justify-content:space-between;gap:16px;align-items:flex-start;
}
.item-main{flex:1;min-width:0;display:flex;flex-direction:column;gap:3px}
.item-title{display:flex;align-items:baseline;gap:10px;flex-wrap:wrap}
.item strong{font-size:15px;font-weight:600}
.actions{display:flex;gap:4px;flex-shrink:0;flex-wrap:wrap;justify-content:flex-end}

/* ---------- Status dots ---------- */
.dot{font-size:13px;font-weight:500;white-space:nowrap}
.dot.ok{color:var(--ok)}
.dot.err{color:var(--err)}
.dot.warn{color:var(--warn)}
.dot.off{color:var(--muted)}
.dot.spin{animation:pulse 1.2s ease-in-out infinite}
@keyframes pulse{50%{opacity:.35}}
.mail-fail{color:var(--err);display:inline-flex;align-items:center}

/* ---------- Cards ---------- */
.card{background:var(--surface);border:1px solid var(--line);border-radius:12px;padding:24px}
.card + .card{margin-top:16px}
.card h3{margin:0 0 18px;font-size:12px;font-weight:600;text-transform:uppercase;letter-spacing:.7px;color:var(--muted)}
.card .list > .item:first-child{border-top:0}
.card .item{padding:12px 0}

/* ---------- Modal ---------- */
.modal{
  position:fixed;inset:0;background:var(--overlay);
  display:flex;align-items:center;justify-content:center;
  z-index:10;padding:24px;
}
.modal .card{width:100%;max-width:600px;max-height:90vh;overflow:auto;box-shadow:var(--shadow)}
.modal h2{margin:0 0 18px;font-size:18px}

/* ---------- Rows ---------- */
.row{display:flex;gap:12px;align-items:flex-start}
.row > *{flex:1;min-width:0}
.row.actions-end{justify-content:flex-end}
.row.actions-end > *{flex:0 0 auto}

/* ---------- Filters ---------- */
.filters{display:flex;gap:10px;align-items:center;margin:4px 0 16px;color:var(--muted)}
.filters select{width:auto;min-width:170px}
.filters #rtotal{margin-left:auto}

/* ---------- Pager ---------- */
.pager{display:flex;gap:4px;justify-content:center;align-items:center;padding:20px 0}
.pager .page{min-width:32px;justify-content:center;border:0;background:none;padding:6px 10px;color:var(--muted);border-radius:6px}
.pager .page:hover{background:var(--hover);color:var(--text)}
.pager .page:disabled{opacity:.4}
.pager .page.current{background:var(--accent);color:var(--accent-contrast);opacity:1}
.pager .page.dots{cursor:default;background:none}

/* ---------- Toasts ---------- */
#toasts{position:fixed;right:16px;bottom:16px;display:flex;flex-direction:column;gap:8px;z-index:50;max-width:360px}
.toast{
  background:var(--surface);
  border:1px solid var(--line-strong);
  border-left:3px solid var(--accent);
  padding:10px 14px;border-radius:var(--radius);
  box-shadow:var(--shadow);font-size:13px;cursor:pointer;
}
.toast.error{border-left-color:var(--err)}

/* ---------- Tag input ---------- */
.tag-input{
  border:1px solid var(--line-strong);
  border-radius:var(--radius);
  padding:6px;margin-top:6px;
  display:flex;gap:6px;flex-wrap:wrap;
  background:var(--surface);
  transition:border-color .12s,box-shadow .12s;
}
.tag-input:focus-within{border-color:var(--accent);box-shadow:0 0 0 3px var(--focus)}
.tag-input input{border:0;flex:1;min-width:140px;padding:4px 6px;outline:none;box-shadow:none;background:none}
.tag-input input:focus{box-shadow:none}
.tag{
  background:var(--hover);color:var(--text);
  padding:3px 10px;border-radius:999px;font-size:13px;
  display:inline-flex;gap:6px;align-items:center;font-weight:500;
}
.tag button{
  background:transparent;border:0;color:var(--muted);
  padding:0;font-size:15px;line-height:1;
  width:16px;height:16px;border-radius:50%;justify-content:center;
}
.tag button:hover{color:var(--err);background:none}

/* ---------- Responsive ---------- */
@media (max-width:720px){
  header{padding:0 16px;gap:12px;height:auto;min-height:56px;flex-wrap:wrap}
  header .brand{padding:12px 0}
  header nav{order:3;flex-basis:100%;overflow-x:auto;height:44px}
  header .user{align-self:center;margin-left:auto}
  main{padding:16px}
  .row{flex-direction:column;gap:14px}
  .item{flex-direction:column}
  .actions{width:100%;justify-content:flex-start}
  .filters{flex-wrap:wrap}
  .filters #rtotal{margin-left:0;flex-basis:100%}
}
```

- [ ] **Step 4: Delete the old monolith**

```bash
git rm internal/handlers/web/app.js
```

- [ ] **Step 5: Build and commit**

Run: `go build ./... && go test ./...`
Expected: build ok, tests pass.

```bash
git add internal/handlers/web/
git commit -m "feat(ui): minimalist redesign — modules, dark theme, icons, filters"
```

---

## Task 6: Manual smoke checklist

Requires Docker + Postgres: `docker compose up -d && go run ./cmd/server`, open http://localhost:8080.

- [ ] **Step 1: Theme**
  1. Toggle cycles монитор → солнце → луна; page switches system → light → dark.
  2. Set «тёмная», reload — the page loads dark with no light flash; `localStorage.theme === 'dark'`.
  3. Set «системная» — `localStorage.theme` removed; page follows OS setting.

- [ ] **Step 2: Дайджесты**
  1. Create a digest (all fields, tags via Enter) → toast «Сохранено», row appears with icons and status dot.
  2. «Запустить» → toast «Запуск поставлен в очередь».
  3. «Удалить» → confirm modal warns history is deleted too; cancel keeps the row, confirm removes it.
  4. With >20 digests (optional, seed via psql) pager appears; page switch keeps working.

- [ ] **Step 3: История**
  1. Filter by digest and by status — list narrows, «всего: N» updates, switching filters resets to page 1.
  2. While a run is `processing` the row shows an amber pulsing «в работе» and the list refreshes itself within ~10 s of completion.
  3. A run with a failed mail shows the red mail icon; hover shows the error.
  4. «Открыть» opens the rendered digest in a new tab (no popup warning).
  5. Pager: with >20 runs pages switch, filters survive page changes.

- [ ] **Step 4: Настройки**
  1. «Хранить историю, дней» saves and survives reload; other settings save with toasts.
  2. Pause/resume toggles with toasts.
  3. User add/edit/delete works; delete asks via confirm modal.

- [ ] **Step 5: Мобильная ширина**
  Narrow the window below 720px — nav scrolls horizontally, rows stack, filters wrap.

- [ ] **Step 6: Fix anything found, re-run `go test ./...`, commit fixes if any**
