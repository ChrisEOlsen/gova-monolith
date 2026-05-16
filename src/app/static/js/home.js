const app = document.getElementById('app');

function render() {
  const h1 = document.createElement('h1');
  h1.className = 'text-2xl font-semibold tracking-tight';
  h1.textContent = 'Welcome';
  app.replaceChildren();
  app.appendChild(h1);
}

render();
