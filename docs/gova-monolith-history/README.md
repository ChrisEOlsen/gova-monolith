# How gova-monolith itself was built

These are the specs, plans and summaries from developing **the template** — the
MCP builder, the API wire contract, the manifest and generated routing, the iOS
client. They are kept because they record why those things are shaped the way
they are.

**None of it describes an app built with the template.**

If you are working in a derived app, this directory is not your project's
history and nothing in it should be treated as a decision your app made. It is
safe to delete outright:

```
git rm -r docs/gova-monolith-history
```

Your own specs and plans belong in `docs/superpowers/`.
