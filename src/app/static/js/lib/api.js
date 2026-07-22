const csrf = () => document.cookie.match(/csrf_token=([^;]+)/)?.[1] ?? '';

export async function get(path) {
  const res = await fetch(path, { credentials: 'same-origin' });
  try {
    return await res.json();
  } catch {
    // Body was not JSON (proxy error page, network truncation) — synthesize
    // an envelope so callers never have to branch on shape.
    return { ok: false, error: `HTTP ${res.status}`, code: 'internal' };
  }
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
