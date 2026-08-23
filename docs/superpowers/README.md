# This app's specs and plans

`gova-brainstorm` writes design docs to `specs/`. `gova-writing-plans` writes
implementation plans to `plans/`. `gova-brainstorm` reads both back as project
history when it starts.

**Both directories are empty in a fresh app, and that is deliberate.**

They used to ship holding ten plans and ten specs describing how gova-monolith
*itself* was built — the manifest work, the wire contract, the iOS client.
Every project scaffolded from this template inherited all of it. Since the
brainstorm step reads this directory as "what has been decided on this
project", an app with one real plan presented eleven, ten of them about
somebody else's codebase. That is context spent to be actively misled.

Anything in here should describe **this** app. If you find a document about
building the template, it does not belong in a project and can be deleted.
