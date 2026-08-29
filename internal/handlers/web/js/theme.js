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
