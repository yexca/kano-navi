# Kano Navi Documentation

These documents are organized by reader: visitors who want to know what the
board offers, developers who change it, and operators who run an instance.
They describe the current system only.

## For Visitors

- [User guide](user/index.md): the board, the schedule, posts, videos, and the
  Journey page.

## For Developers

- [How it works](how-it-works.md): goals, data flow, operator console, and
  known limits.
- [Architecture](architecture/index.md): backend, APIs, frontend, schema,
  sources, workflows, and media.
- [Development](development/index.md): local setup, design, testing, history
  archive maintenance, commits, and releases.
- [Repository agent guide](../AGENTS.md): boundaries and validation rules.
- [Decisions](decisions/index.md): durable architecture trade-offs.

## For Operators

- [Operations](operations/index.md): configuration, Docker, database and
  backups, reliability, deployment security, and troubleshooting.
- [Security](security/index.md): reporting policy and security boundaries.
- [Release history](history/index.md): release notes and upgrade steps.

## Documentation Rules

Where each topic belongs:

- Features as visitors see them belong in the [User guide](user/index.md),
  without implementation detail.
- How the system works belongs in [How it works](how-it-works.md); module, API
  and data contracts belong in [Architecture](architecture/index.md), one
  topic per document.
- Runtime setup and recovery belong in [Operations](operations/index.md).
- Local workflow, checks, content maintenance and contribution rules belong in
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
