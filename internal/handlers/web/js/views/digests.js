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
  function redraw(focus = false) {
    wrap.innerHTML = '';
    values.forEach(v => {
      const t = document.createElement('span'); t.className = 'tag';
      t.innerHTML = esc(v) + ' <button type="button">×</button>';
      // mousedown, не click: срабатывает до blur инпута, пока кнопка ещё жива.
      t.querySelector('button').addEventListener('mousedown', e => {
        e.preventDefault();
        values.delete(v); redraw(true);
      });
      wrap.appendChild(t);
    });
    wrap.appendChild(input);
    if (focus) input.focus();
  }
  input.addEventListener('keydown', e => {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault();
      const v = input.value.trim();
      if (v) { values.add(v); input.value = ''; redraw(true); }
    } else if (e.key === 'Backspace' && !input.value && values.size) {
      const last = [...values].pop(); values.delete(last); redraw(true);
    }
  });
  input.addEventListener('blur', () => {
    const v = input.value.trim();
    if (v) { values.add(v); input.value = ''; redraw(false); }
  });
  redraw(false);
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
      <button type="button" id="d-cancel">Отмена</button>
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
