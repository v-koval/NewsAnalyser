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
        <button type="button" data-act="cancel">Отмена</button>
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
