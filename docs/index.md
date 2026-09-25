# Documentation Index

This is the entry point for all project documentation. **Before you change code, find the matching doc below and read
it first.** Repo-wide rules that apply to every change (commands, Git policy, docs-sync) live in
[AGENTS.md](../AGENTS.md).

## Reference docs

Repo-wide conventions. Read the matching doc before working in that area.

| When you are…                                                                | Read                                              |
|------------------------------------------------------------------------------|---------------------------------------------------|
| Adding a feature, moving code between layers, or touching `main.go`          | [architecture.md](./architecture.md)              |
| Writing or changing a handler, request DTO, response shape or list endpoint  | [http-conventions.md](./http-conventions.md)      |
| Adding or changing log calls                                                 | [logging.md](./logging.md)                        |
| Adding, renaming or removing a config key or env var                         | [configuration.md](./configuration.md)            |

## Feature docs

Per-feature endpoints, code map, data, caching, and gotchas. Start at the
[feature index](./features/index.md), which lists every feature, its endpoints, its main packages, the dependencies
between features, and the template for a new feature doc.

## Keeping this index current

- Adding a reference doc → add a row to the table above.
- Adding a feature doc → add it to [features/index.md](./features/index.md), not here.
