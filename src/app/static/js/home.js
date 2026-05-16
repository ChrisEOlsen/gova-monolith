const app = document.getElementById('app');

function render() {
  const steps = [
    { label: 'Step 1: Database',  body: 'Use ', code1: 'execute_sql', mid: ' to create your tables. Every table needs an ', code2: 'id INTEGER PRIMARY KEY', end: '.' },
    { label: 'Step 2: Scaffold',  body: 'Use ', code1: 'scaffold_list', mid: ' or ', code2: 'create_model', end: ' to generate models and JSON handlers.' },
    { label: 'Step 3: Interface', body: 'Inject forms with ', code1: 'add_js_form', mid: ' and build custom pages with ', code2: 'create_page', end: '.' },
    { label: 'Step 4: Polish',    body: 'Compile styles with ', code1: 'build_css', mid: ' and wire auth with ', code2: 'scaffold_auth', end: ' if needed.' },
  ];

  const links = [
    { label: 'Go Docs',       href: 'https://pkg.go.dev' },
    { label: 'chi Router',    href: 'https://github.com/go-chi/chi' },
    { label: 'Tailwind CSS',  href: 'https://tailwindcss.com' },
    { label: 'SQLite',        href: 'https://sqlite.org' },
  ];

  function code(text) {
    const c = document.createElement('code');
    c.className = 'font-mono text-gray-700 bg-gray-100 rounded px-1 py-0.5 text-xs';
    c.textContent = text;
    return c;
  }

  // Hero
  const hero = document.createElement('div');
  hero.className = 'text-center py-12';
  const h1 = document.createElement('h1');
  h1.className = 'text-4xl font-bold tracking-tight text-gray-900 mb-3';
  h1.textContent = 'Welcome to GOVA';
  const tagline = document.createElement('p');
  tagline.className = 'text-gray-500 text-lg';
  tagline.textContent = 'Your AI-first Go + Vanilla JS template is ready.';
  hero.append(h1, tagline);

  // Getting started card
  const card = document.createElement('section');
  card.className = 'bg-white border border-gray-200 rounded-lg p-8 shadow-sm';
  const h2 = document.createElement('h2');
  h2.className = 'text-lg font-semibold mb-6';
  h2.textContent = 'Getting Started';
  const grid = document.createElement('div');
  grid.className = 'grid gap-6 md:grid-cols-2';

  steps.forEach(s => {
    const div = document.createElement('div');
    div.className = 'space-y-1';
    const h3 = document.createElement('h3');
    h3.className = 'text-xs font-bold text-blue-600 uppercase tracking-wider';
    h3.textContent = s.label;
    const p = document.createElement('p');
    p.className = 'text-sm text-gray-500';
    p.append(s.body, code(s.code1), s.mid, code(s.code2), s.end);
    div.append(h3, p);
    grid.appendChild(div);
  });

  card.append(h2, grid);

  // Footer links
  const linksRow = document.createElement('div');
  linksRow.className = 'flex justify-center gap-8';
  links.forEach(l => {
    const a = document.createElement('a');
    a.className = 'text-xs font-medium text-gray-400 hover:text-blue-500 transition-colors';
    a.href = l.href;
    a.target = '_blank';
    a.rel = 'noopener noreferrer';
    a.textContent = l.label;
    linksRow.appendChild(a);
  });

  const wrapper = document.createElement('div');
  wrapper.className = 'py-12 space-y-10';
  wrapper.append(hero, card, linksRow);

  app.replaceChildren(wrapper);
}

render();
