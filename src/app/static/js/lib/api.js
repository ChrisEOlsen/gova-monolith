const csrf = () => document.cookie.match(/csrf_token=([^;]+)/)?.[1] ?? '';

export async function get(path) {
  const res = await fetch(path, { credentials: 'same-origin' });
  if (!res.ok && res.status !== 401) {
    return { ok: false, error: `HTTP ${res.status}` };
  }
  return res.json();
}

export async function post(path, body = {}) {
  const res = await fetch(path, {
    method: 'POST',
    credentials: 'same-origin',
    headers: {
      'Content-Type': 'application/json',
      'X-CSRF-Token': csrf(),
    },
    body: JSON.stringify(body),
  });
  return res.json();
}

export async function put(path, body = {}) {
  const res = await fetch(path, {
    method: 'PUT',
    credentials: 'same-origin',
    headers: {
      'Content-Type': 'application/json',
      'X-CSRF-Token': csrf(),
    },
    body: JSON.stringify(body),
  });
  return res.json();
}

export async function del(path) {
  const res = await fetch(path, {
    method: 'DELETE',
    credentials: 'same-origin',
    headers: { 'X-CSRF-Token': csrf() },
  });
  return res.json();
}
