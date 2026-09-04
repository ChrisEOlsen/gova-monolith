const csrf = () => document.cookie.match(/csrf_token=([^;]+)/)?.[1] ?? '';

// envelope turns a Response into the API's { ok, ... } shape.
//
// A response can arrive without one — a proxy's HTML error page, a 502 from a
// restarting container, a cut connection — and res.json() throws on all three.
// Synthesizing an envelope instead means every function here resolves to an
// object with .ok, so no caller needs a try/catch. A throw would skip both the
// button re-enable and the error branch, freezing the form silently.
async function envelope(res) {
  try {
    return await res.json();
  } catch {
    return { ok: false, error: `HTTP ${res.status}`, code: 'internal' };
  }
}

// Every state-changing request carries the JSON content type and the CSRF
// token read back out of the double-submit cookie.
const writeHeaders = () => ({
  'Content-Type': 'application/json',
  'X-CSRF-Token': csrf(),
});

export async function get(path) {
  const res = await fetch(path, { credentials: 'same-origin' });
  return envelope(res);
}

export async function post(path, body = {}) {
  const res = await fetch(path, {
    method: 'POST',
    credentials: 'same-origin',
    headers: writeHeaders(),
    body: JSON.stringify(body),
  });
  return envelope(res);
}

export async function put(path, body = {}) {
  const res = await fetch(path, {
    method: 'PUT',
    credentials: 'same-origin',
    headers: writeHeaders(),
    body: JSON.stringify(body),
  });
  return envelope(res);
}

export async function del(path) {
  const res = await fetch(path, {
    method: 'DELETE',
    credentials: 'same-origin',
    headers: { 'X-CSRF-Token': csrf() },
  });
  return envelope(res);
}
