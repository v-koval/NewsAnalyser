const state = {
  access: localStorage.getItem('access') || '',
  refresh: localStorage.getItem('refresh') || '',
  user: null,
};

function setTokens(access, refresh) {
  state.access = access; state.refresh = refresh;
  if (access) localStorage.setItem('access', access); else localStorage.removeItem('access');
  if (refresh) localStorage.setItem('refresh', refresh); else localStorage.removeItem('refresh');
}

async function api(path, opts = {}) {
  opts.headers = Object.assign({'Content-Type': 'application/json'}, opts.headers || {});
  if (state.access) opts.headers['Authorization'] = 'Bearer ' + state.access;
  let res = await fetch(path, opts);
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
    logout(); throw new Error('unauthorized');
  }
  if (!res.ok) {
    let msg = res.statusText;
    try { const j = await res.json(); msg = j.error || msg; } catch {}
    throw new Error(msg);
  }
  if (res.status === 204) return null;
  return res.json();
}

function $(sel, root = document) { return root.querySelector(sel); }
function $$(sel, root = document) { return [...root.querySelectorAll(sel)]; }

function esc(s) { return String(s == null ? '' : s).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'})[c]); }

// ---------- Auth ----------

$('#login-form').addEventListener('submit', async (e) => {
  e.preventDefault();
  const fd = new FormData(e.target);
  try {
    const j = await (await fetch('/api/auth/login', {
      method: 'POST', headers: {'Content-Type':'application/json'},
      body: JSON.stringify({email: fd.get('email'), password: fd.get('password')}),
    })).json().then(r => { if (r.error) throw new Error(r.error); return r; });
    setTokens(j.access, j.refresh);
    state.user = j.user;
    showApp();
  } catch (err) { $('#login-err').textContent = err.message; }
});

$('#logout').addEventListener('click', logout);

async function logout() {
  if (state.refresh) {
    try { await fetch('/api/auth/logout', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({refresh: state.refresh})}); } catch {}
  }
  setTokens('', ''); state.user = null; showLogin();
}

function showLogin() {
  $('#app').classList.add('hidden'); $('#login').classList.remove('hidden');
}
function showApp() {
  $('#login').classList.add('hidden'); $('#app').classList.remove('hidden');
  if (state.user) $('#user-email').textContent = state.user.email;
  route();
}

async function boot() {
  if (!state.access) { showLogin(); return; }
  try { state.user = await api('/api/me'); showApp(); }
  catch { showLogin(); }
}

// ---------- Routing ----------

window.addEventListener('hashchange', route);
function route() {
  if (!state.access) { showLogin(); return; }
  const h = location.hash || '#/digests';
  $$('header nav a').forEach(a => a.classList.toggle('active', h.startsWith(a.getAttribute('href'))));
  if (h.startsWith('#/runs')) renderRuns();
  else if (h.startsWith('#/settings')) renderSettings();
  else renderDigests();
}

// ---------- Digests ----------

async function renderDigests() {
  const v = $('#view');
  v.innerHTML = `<div class="section-head"><h2>Дайджесты</h2><button id="new-digest">+ Новый дайджест</button></div><div id="dlist" class="list">Загрузка…</div>`;
  $('#new-digest').onclick = () => openDigestModal();
  try {
    const list = (await api('/api/digests?per_page=100')).items;
    $('#dlist').innerHTML = list.length ? '' : '<p>Пока ничего нет.</p>';
    list.forEach(d => {
      const el = document.createElement('div');
      el.className = 'item';
      el.innerHTML = `
        <div>
          <div><strong>${esc(d.name)}</strong>
            <span class="badge ${d.enabled?'':'off'}">${d.enabled?'включен':'выключен'}</span>
            <span class="badge off">каждые ${d.frequency_hours} ч</span>
            <span class="badge off">${esc(d.language)}</span>
            <span class="badge off">${(d.kind || 'news') === 'facts' ? 'Факты' : 'Новости'}</span>
          </div>
          <div class="meta">${esc(d.topic || '')}</div>
          <div class="meta">Получатели: ${d.recipients.map(esc).join(', ') || '—'}</div>
          <div class="meta">Источники: ${(d.sources.length?d.sources:d.auto_sources).map(esc).join(', ') || '— будут подобраны автоматически'}</div>
          <div class="meta">Последний запуск: ${d.last_run_at ? new Date(d.last_run_at).toLocaleString() : '—'}</div>
          <div class="meta">Следующий запуск: ${d.next_run_at ? new Date(d.next_run_at).toLocaleString() : '—'}</div>
        </div>
        <div class="actions">
          <button class="secondary" data-act="run">Запустить</button>
          <button class="secondary" data-act="edit">Ред.</button>
          <button class="danger" data-act="del">Удалить</button>
        </div>`;
      el.querySelector('[data-act=run]').onclick = async () => { await api('/api/digests/'+d.id+'/run', {method:'POST'}); alert('Поставлено в очередь'); };
      el.querySelector('[data-act=edit]').onclick = () => openDigestModal(d);
      el.querySelector('[data-act=del]').onclick = async () => { if (confirm('Удалить?')) { await api('/api/digests/'+d.id, {method:'DELETE'}); renderDigests(); } };
      $('#dlist').appendChild(el);
    });
  } catch (e) { $('#dlist').innerHTML = `<p class="err">${esc(e.message)}</p>`; }
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
  input.addEventListener('blur', () => { const v = input.value.trim(); if (v){values.add(v); input.value=''; redraw();} });
  redraw();
  return { el: wrap, get: () => [...values] };
}

function openDigestModal(d) {
  const editing = !!d;
  d = d || {name:'',topic:'',sources:[],ignored_sources:[],frequency_hours:24,recipients:[],language:'ru',kind:'news',enabled:true};
  const modal = document.createElement('div'); modal.className = 'modal';
  modal.innerHTML = `<form class="card"><h2>${editing?'Редактирование':'Новый'} дайджест</h2>
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
      <button type="submit">Сохранить</button>
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
      if (editing) await api('/api/digests/'+d.id, {method:'PUT', body: JSON.stringify(payload)});
      else await api('/api/digests', {method:'POST', body: JSON.stringify(payload)});
      modal.remove(); renderDigests();
    } catch (err) { modal.querySelector('#d-err').textContent = err.message; }
  };
}

// ---------- Runs ----------

async function renderRuns() {
  const v = $('#view');
  v.innerHTML = `<div class="section-head"><h2>Составленные дайджесты</h2></div><div id="rlist" class="list">Загрузка…</div>`;
  try {
    const list = (await api('/api/runs?per_page=100')).items;
    if (!list.length) { $('#rlist').innerHTML = '<p>Пока ничего нет.</p>'; return; }
    $('#rlist').innerHTML = '';
    list.forEach(r => {
      const el = document.createElement('div'); el.className = 'item';
      const badge = r.status === 'ok' ? '' : `<span class="badge err">${esc(r.status)}</span>`;
      el.innerHTML = `
        <div>
          <div><strong>${esc(r.digest_name)}</strong> ${badge}</div>
          <div class="meta">Обработано: ${new Date(r.processed_at).toLocaleString()}</div>
          <div class="meta">Период: ${new Date(r.period_from).toLocaleString()} — ${new Date(r.period_to).toLocaleString()}</div>
          <div class="meta">Источники: ${(r.analyzed_sources||[]).map(esc).join(', ') || '—'}</div>
          ${r.error?`<div class="meta err">${esc(r.error)}</div>`:''}
        </div>
        <div class="actions"><button class="secondary" data-act="open">Открыть</button></div>`;
      el.querySelector('[data-act=open]').onclick = async () => {
        const win = window.open('about:blank', '_blank');
        try {
          const j = await api('/api/runs/' + r.id + '/view-link');
          win.location = j.url;
        } catch (e) {
          if (win) win.close();
          alert(e.message);
        }
      };
      $('#rlist').appendChild(el);
    });
  } catch (e) { $('#rlist').innerHTML = `<p class="err">${esc(e.message)}</p>`; }
}

// ---------- Settings ----------

async function renderSettings() {
  const v = $('#view');
  v.innerHTML = `<div class="section-head"><h2>Настройки</h2></div>
    <div class="card"><h3>Обработка</h3>
      <div class="row actions-end"><button id="toggle-proc" class="secondary">…</button></div>
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
        <div class="err" id="s-err"></div>
        <div class="row actions-end"><button type="submit">Сохранить</button></div>
      </form>
    </div>
    <div class="card"><h3>Пользователи</h3>
      <div id="ulist" class="list">Загрузка…</div>
      <div class="row actions-end" style="margin-top:14px"><button id="new-user">+ Добавить пользователя</button></div>
    </div>`;
  const s = await api('/api/settings');
  const form = $('#settings-form');
  form.cursor_api_key.value = ''; form.cursor_api_key.placeholder = s.cursor_api_key ? 'скрыто — оставьте пустым' : 'введите ключ';
  form.cursor_repository.value = s.cursor_repository || '';
  form.smtp_host.value = s.smtp_host || ''; form.smtp_port.value = s.smtp_port || 587;
  form.smtp_user.value = s.smtp_user || ''; form.smtp_password.value = ''; form.smtp_password.placeholder = s.smtp_password ? 'скрыто' : '';
  form.smtp_from.value = s.smtp_from || ''; form.smtp_tls.checked = !!s.smtp_tls;
  const btn = $('#toggle-proc');
  btn.textContent = s.processing_paused ? 'Возобновить обработку' : 'Приостановить обработку';
  btn.onclick = async () => {
    const cur = await api('/api/settings');
    cur.processing_paused = !cur.processing_paused;
    cur.cursor_api_key = ''; cur.smtp_password = '';
    await api('/api/settings', {method:'PUT', body: JSON.stringify(cur)});
    renderSettings();
  };
  form.onsubmit = async (e) => {
    e.preventDefault();
    const payload = {
      cursor_api_key: form.cursor_api_key.value,
      cursor_repository: form.cursor_repository.value,
      smtp_host: form.smtp_host.value, smtp_port: parseInt(form.smtp_port.value,10)||587,
      smtp_user: form.smtp_user.value, smtp_password: form.smtp_password.value,
      smtp_from: form.smtp_from.value, smtp_tls: form.smtp_tls.checked,
      processing_paused: s.processing_paused,
      keep_runs_days: s.keep_runs_days || 0,
    };
    try { await api('/api/settings', {method:'PUT', body: JSON.stringify(payload)}); $('#s-err').textContent = 'Сохранено'; renderSettings(); }
    catch (err) { $('#s-err').textContent = err.message; }
  };
  const users = await api('/api/users');
  $('#ulist').innerHTML = '';
  users.forEach(u => {
    const el = document.createElement('div'); el.className = 'item';
    el.innerHTML = `<div><strong>${esc(u.email)}</strong><div class="meta">${new Date(u.created_at).toLocaleString()}</div></div>
      <div class="actions">
        <button class="secondary" data-act="edit">Ред.</button>
        <button class="danger" data-act="del">Удалить</button>
      </div>`;
    el.querySelector('[data-act=edit]').onclick = () => openUserModal(u);
    el.querySelector('[data-act=del]').onclick = async () => { if (confirm('Удалить?')) { try { await api('/api/users/'+u.id,{method:'DELETE'}); renderSettings(); } catch(e){alert(e.message);} } };
    $('#ulist').appendChild(el);
  });
  $('#new-user').onclick = () => openUserModal();
}

function openUserModal(u) {
  const editing = !!u;
  const modal = document.createElement('div'); modal.className = 'modal';
  modal.innerHTML = `<form class="card"><h2>${editing?'Изменить':'Новый'} пользователь</h2>
    <label>Email<input name="email" type="email" required></label>
    <label>Пароль ${editing?'(пусто — не менять)':''}<input name="password" type="password" ${editing?'':'required'}></label>
    <div class="err" id="u-err"></div>
    <div class="row actions-end">
      <button type="button" class="secondary" id="u-cancel">Отмена</button>
      <button type="submit">Сохранить</button>
    </div></form>`;
  document.body.appendChild(modal);
  if (editing) modal.querySelector('[name=email]').value = u.email;
  modal.querySelector('#u-cancel').onclick = () => modal.remove();
  modal.querySelector('form').onsubmit = async (e) => {
    e.preventDefault();
    const f = e.target;
    const payload = {email: f.email.value.trim(), password: f.password.value};
    try {
      if (editing) await api('/api/users/'+u.id, {method:'PUT', body: JSON.stringify(payload)});
      else await api('/api/users', {method:'POST', body: JSON.stringify(payload)});
      modal.remove(); renderSettings();
    } catch (err) { modal.querySelector('#u-err').textContent = err.message; }
  };
}

boot();
