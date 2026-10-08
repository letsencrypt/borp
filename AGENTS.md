# Agent Guidance

This file gives automated coding agents lightweight project context. Maintainer
review and the repository documentation remain authoritative.

- Start with `README.md` for setup, supported dialects, and test commands.
- For generated SQL changes, use `docs/sql-generation-review.md`.
- Prefer small changes that follow the existing `DbMap`, `TableMap`, `ColumnMap`,
  `IndexMap`, and dialect patterns.
- Add focused tests for changed behavior; include affected dialects when SQL
  syntax or quoting differs by dialect.
- Keep tests at the public API level where practical.
- Run `gofmt` and relevant `go test` commands before submitting changes.
