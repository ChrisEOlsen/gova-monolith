const csrf = () => document.cookie.match(/csrf_token=([^;]+)/)?.[1] ?? '';

// envelope turns a Response into the API's { ok, ... } envelope.
//
// Every handler answers with that envelope, but a response can still arrive
// without one: a proxy or load balancer returning its own HTML error page, a
// 502 from a restarting container, a connection cut mid-body. res.json() throws
// on all three.
//
// A throw is much worse here than a false-y result, because of how every
// generated caller is written:
//
//     const res = await post(endpoint, data);
//     submitBtn.disabled = false;
//     if (res.ok) { ... } else { errEl.textContent = res.error }
//
// The throw skips the re-enable AND the error branch, so the form freezes with
// its button disabled and says nothing. The user has no way forward but a
// reload. get() has been guarded since the start; post/put/del were not, so the
// same backend hiccup produced a clean message on a read and a dead form on a
// write.
//
// Synthesizing an envelope keeps that promise total: every function in this
// module resolves to an object with .ok, so no caller ever needs a try/catch or
// a shape check.
async function envelope(res) {
  try {
    return await res.json();
  } catch {
    return { ok: false, error: `HTTP ${res.status}`, code: 'internal' };
  }
}

// writeHeaders are the headers every state-changing request carries: the JSON
// content type, and the CSRF token read back out of the double-submit cookie.
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
