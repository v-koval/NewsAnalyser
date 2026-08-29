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
