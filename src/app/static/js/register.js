import { post } from '/static/js/lib/api.js';
import { redirectIfAuthed } from '/static/js/lib/auth.js';

const form = document.getElementById('register-form');
const errorMsg = document.getElementById('error-msg');
const submitBtn = form.querySelector('button[type="submit"]');

form.addEventListener('submit', async (e) => {
  e.preventDefault();
  errorMsg.classList.add('hidden');
  submitBtn.disabled = true;

  const res = await post('/api/v1/auth/register', {
    name: document.getElementById('name').value,
    email: document.getElementById('email').value,
    password: document.getElementById('password').value,
  });

  submitBtn.disabled = false;
  if (res.ok) {
    window.location.href = '/';
    return;
  }
  errorMsg.textContent = res.error ?? 'Registration failed.';
  errorMsg.classList.remove('hidden');
});

redirectIfAuthed();
