import { get } from './api.js';

export async function requireAuth() {
  const res = await get('/api/v1/auth/me');
  if (!res.ok) {
    window.location.href = '/static/pages/login.html';
    return null;
  }
  return res.data;
}

export async function redirectIfAuthed() {
  const res = await get('/api/v1/auth/me');
  if (res.ok) {
    window.location.href = '/static/pages/home.html';
  }
}
