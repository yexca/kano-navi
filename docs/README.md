# Kano Navi Documentation

These documents are organized by reader task: understand the board, operate a
deployment, change code, or review security and design decisions. They
describe the current system only.

## Start Here

- [Project overview](overview.md): scope, pages, current behavior and limits.
- [Local development](development/local-dev.md): setup and change workflow.
- [Docker deployment](operations/docker.md): running and updating an instance.
- [Repository agent guide](../AGENTS.md): boundaries and validation rules.

## By Area

| Area         | Entry point                               | Contents                                                     |
| ------------ | ----------------------------------------- | ------------------------------------------------------------ |
| Architecture | [Architecture map](architecture/index.md) | Backend, APIs, frontend, schema, sources, workflows, media   |
| Product      | [Product map](product/index.md)           | Board scope and curated history provenance                   |
| Operations   | [Operations map](operations/index.md)     | Configuration, Docker, backup, reliability, troubleshooting  |
| Development  | [Development map](development/index.md)   | Setup, design, testing, secure changes, commits and releases |
| Security     | [Security map](security/index.md)         | Reporting policy, deployment and development boundaries      |
| Decisions    | [ADR index](decisions/index.md)           | Durable architecture trade-offs                              |
| History      | [Release history](history/index.md)       | Release notes and upgrade guidance                           |

## Reading Paths

- Taking over the project: overview, core boundaries, data model, sources,
  workflows, and testing.
- Changing public or admin UI: frontend, design, and testing.
- Changing an HTTP or MCP contract: backend, public API or admin API, and
  secure development.
- Changing fetch or extraction behavior: sources, workflows, data model, media,
  and secure development.
- Operating a deployment: configuration, Docker, database, reliability, and
  deployment security.
- Preparing a release: testing, commit and release, and
  [release history](history/index.md).

## Documentation Rules

Where each topic belongs:

- Visitor-visible behavior belongs in [Overview](overview.md) or
  [Product](product/index.md).
- Module, API and data contracts belong in
  [Architecture](architecture/index.md), one topic per document.
- Runtime setup and recovery belong in [Operations](operations/index.md).
- Local workflow, checks and contribution rules belong in
  [Development](development/index.md).
- Security reporting belongs in [SECURITY.md](../SECURITY.md); technical
  boundaries belong in [Deployment security](operations/security.md) and
  [Secure development](development/security.md).
- Durable design choices belong in [ADRs](decisions/index.md).
- Release notes, pending changes, and per-release upgrade steps belong in
  [History](history/index.md). Every other page describes only the current
  system; see the writing rules in the
  [repository agent guide](../AGENTS.md#documentation).
- [.env.example](../.env.example) is the complete runtime variable inventory;
  [Configuration](operations/configuration.md) documents each variable.
- Documentation is English only. Use repository-relative paths, synthetic
  examples, and empty credential fields, and run `make docs-check` after
  changing links.
