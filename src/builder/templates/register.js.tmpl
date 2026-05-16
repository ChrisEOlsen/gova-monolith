import { post } from '/static/js/lib/api.js';
import { redirectIfAuthed } from '/static/js/lib/auth.js';

const form = document.getElementById('register-form');
const errorMsg = document.getElementById('error-msg');

async function init() {
  await redirectIfAuthed();

  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    errorMsg.classList.add('hidden');
    const name = document.getElementById('name').value;
    const email = document.getElementById('email').value;
    const password = document.getElementById('password').value;

    const res = await post('/api/auth/register', { name, email, password });
    if (res.ok) {
      window.location.href = '/static/pages/home.html';
    } else {
      errorMsg.textContent = res.error ?? 'Registration failed.';
      errorMsg.classList.remove('hidden');
    }
  });
}

init();
