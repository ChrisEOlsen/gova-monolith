# TODO

- [ ] Add a backend-only Go test suite (models/handlers, `testing` + `httptest`, in-memory/throwaway SQLite for tests — no `/data/app.db` writes). Skip JS testing (blocked by the no-Node/npm constraint in CLAUDE.md). Also needs: test `.tmpl` companions in `src/builder/templates/` so scaffolded models/handlers generate tests, and a follow-up pass on `gova-writing-plans`/`gova-build-execution` to reinstate TDD-shaped task steps.
