import { post } from '/static/js/lib/api.js';
import { redirectIfAuthed } from '/static/js/lib/auth.js';

const form = document.getElementById('login-form');
const errorMsg = document.getElementById('error-msg');

async function init() {
  await redirectIfAuthed();

  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    errorMsg.classList.add('hidden');
    const email = document.getElementById('email').value;
    const password = document.getElementById('password').value;

    const res = await post('/api/auth/login', { email, password });
    if (res.ok) {
      window.location.href = '/static/pages/home.html';
    } else {
      errorMsg.textContent = res.error ?? 'Login failed.';
      errorMsg.classList.remove('hidden');
    }
  });
}

init();
