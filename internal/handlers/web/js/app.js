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
