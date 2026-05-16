const app = document.getElementById('app');

function render() {
  const sections = [
    {
      heading: 'First-Time Setup',
      items: [
        'Clone this repo',
        ['Run the install script for your harness: ', 'install-claude.sh', ' · ', 'install-opencode.sh', ' · ', 'install-gemini.sh'],
        'Open your AI tool in this directory',
        ['Verify MCP tools are connected: ', '/mcp', ' → should show ', 'gova-builder', ' tools'],
      ],
    },
    {
      heading: 'Before /build',
      items: [
        ['Fill in ', 'SEED.md', ' with app name, features, auth requirements'],
        ['.env', ' has all required API keys for integrations in SEED.md'],
      ],
    },
    {
      heading: 'Before /launch',
      items: [
        ['App reviewed and working at ', 'http://localhost:[APP_PORT]'],
        ['Set ', 'TUNNEL_TOKEN', ' in ', '.env'],
        'Domain configured in Cloudflare dashboard (Zero Trust → Tunnels)',
      ],
    },
  ];

  function code(text) {
    const c = document.createElement('code');
    c.className = 'font-mono text-gray-700 bg-gray-100 rounded px-1 py-0.5 text-xs';
    c.textContent = text;
    return c;
  }

  function makeItem(item) {
    const li = document.createElement('li');
    li.className = 'flex items-start gap-2 text-sm text-gray-600';
    const check = document.createElement('span');
    check.className = 'mt-0.5 text-gray-300 select-none';
    check.textContent = '☐';
    li.appendChild(check);
    const span = document.createElement('span');
    if (typeof item === 'string') {
      span.textContent = item;
    } else {
      item.forEach((part, i) => {
        span.appendChild(i % 2 === 0 ? document.createTextNode(part) : code(part));
      });
    }
    li.appendChild(span);
    return li;
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

  // Checklist card
  const card = document.createElement('section');
  card.className = 'bg-white border border-gray-200 rounded-lg p-8 shadow-sm space-y-8';

  sections.forEach(s => {
    const div = document.createElement('div');
    const h2 = document.createElement('h2');
    h2.className = 'text-xs font-bold text-blue-600 uppercase tracking-wider mb-3';
    h2.textContent = s.heading;
    const ul = document.createElement('ul');
    ul.className = 'space-y-2';
    s.items.forEach(item => ul.appendChild(makeItem(item)));
    div.append(h2, ul);
    card.appendChild(div);
  });

  const wrapper = document.createElement('div');
  wrapper.className = 'py-12 space-y-10';
  wrapper.append(hero, card);

  app.replaceChildren(wrapper);
}

render();
