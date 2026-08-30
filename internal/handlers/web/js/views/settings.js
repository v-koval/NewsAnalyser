import {api} from '../api.js';
import {$, esc, icon, toast, confirmDialog} from '../ui.js';

export async function renderSettings(view) {
  view.innerHTML = `<div class="section-head"><h2>Настройки</h2></div>
    <div class="card"><h3>Обработка</h3>
      <div class="row">
        <label>Хранить историю, дней (0 — вечно)<input type="number" min="0" id="keep-days"></label>
      </div>
      <div class="row actions-end">
        <button id="toggle-proc">…</button>
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

    $('#new-user').onclick = () => openUserModal(null, view);
    try {
      await renderUsers(view);
    } catch (e) {
      $('#ulist').innerHTML = `<p class="err">${esc(e.message)}</p>`;
    }
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
      <button type="button" id="u-cancel">Отмена</button>
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
