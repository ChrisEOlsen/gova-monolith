# This app's specs and plans

`gova-brainstorm` writes design docs to `specs/`, `gova-writing-plans` writes
implementation plans to `plans/`, and `gova-brainstorm` reads both back as
project history when it starts.

**Both directories start empty in a fresh app, and that is deliberate.** They
used to ship holding ten plans and ten specs describing how gova-monolith
*itself* was built — the manifest work, the wire contract, the iOS client. In a
derived app those read as that app's own history, which is exactly what the
brainstorm step goes looking for. An app with one real plan had eleven files
here, ten of them about somebody else's project.

That history still exists, in `docs/gova-monolith-history/`. It is about the
template, not about anything built with it.
