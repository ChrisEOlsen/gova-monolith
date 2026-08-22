import { get } from './api.js';

// Both redirects below target ROUTES, not the raw files behind them.
//
// /login is the path scaffold_auth registers in api.json's pages table, and /
// is wired to the home shell in main.go. The static file server also exposes
// /static/pages/login.html directly, which is what these two lines used to
// point at — it worked, but it meant every app had two live URLs for the same
// page and this module used the one no route owned. A page that later takes
// auth: true is guarded at its route and not under /static/, so the raw path is
// also the one that would quietly skip the guard.
export async function requireAuth() {
  const res = await get('/api/v1/auth/me');
  if (!res.ok) {
    window.location.href = '/login';
    return null;
  }
  return res.data;
}

export async function redirectIfAuthed() {
  const res = await get('/api/v1/auth/me');
  if (res.ok) {
    window.location.href = '/';
  }
}
