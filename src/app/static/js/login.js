import { post } from '/static/js/lib/api.js';
import { redirectIfAuthed } from '/static/js/lib/auth.js';

const form = document.getElementById('login-form');
const errorMsg = document.getElementById('error-msg');
const submitBtn = form.querySelector('button[type="submit"]');

form.addEventListener('submit', async (e) => {
  e.preventDefault();
  errorMsg.classList.add('hidden');
  submitBtn.disabled = true;

  const res = await post('/api/v1/auth/login', {
    email: document.getElementById('email').value,
    password: document.getElementById('password').value,
  });

  submitBtn.disabled = false;
  if (res.ok) {
    window.location.href = '/';
    return;
  }
  errorMsg.textContent = res.error ?? 'Login failed.';
  errorMsg.classList.remove('hidden');
});

redirectIfAuthed();
