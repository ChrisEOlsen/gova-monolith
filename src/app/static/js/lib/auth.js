import { get } from './api.js';

// Both redirects target routes, not the raw files behind them: /login is the
// path api.json registers, and a page that later takes auth:true is guarded at
// its route, not under /static/.
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
