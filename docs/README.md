# Kano Navi Documentation

These documents are organized by reader task: understand the board, operate a
deployment, change code, or review security and design decisions.

## Start Here

- [Project overview](overview.md): scope, pages, current behavior and limits.
- [Local development](development/local-dev.md): setup and change workflow.
- [Docker deployment](operations/docker.md): running and updating an instance.
- [Repository Agent guide](../AGENTS.md): boundaries and validation rules.

## By Area

| Area         | Entry point                               | Contents                                                     |
| ------------ | ----------------------------------------- | ------------------------------------------------------------ |
| Architecture | [Architecture map](architecture/index.md) | Browser/API boundary, schema, sources, workflows, media      |
| Product      | [Product map](product/index.md)           | Board scope and curated history provenance                   |
| Operations   | [Operations map](operations/index.md)     | Configuration, Docker, backup, reliability, troubleshooting  |
| Development  | [Development map](development/index.md)   | Setup, design, testing, secure changes, commits and releases |
| Security     | [Security map](security/index.md)         | Reporting policy, deployment and development boundaries      |
| Decisions    | [ADR index](decisions/index.md)           | Durable architecture trade-offs                              |

## Reading Paths

- Taking over the project: overview, core boundaries, data model, sources,
  workflows, and testing.
- Changing public or admin UI: frontend ownership, design, and testing.
- Changing fetch or extraction behavior: sources, workflows, data model, media,
  and secure development.
- Operating a deployment: configuration, Docker, database, reliability, and
  deployment security.
- Preparing a release: testing, commit and release, and deployment instructions.

## Documentation Rules

- Visitor-visible behavior belongs in overview or product; module and data
  contracts belong in architecture.
- Runtime setup and recovery belong in operations; local workflow and checks
  belong in development.
- Security reporting belongs in SECURITY.md; technical boundaries belong in the
  linked deployment and development security documents.
- Record durable trade-offs as ADRs. Update current contracts with code changes;
  do not infer current behavior from historical notes.
- Use repository-relative paths, synthetic examples, and empty credential fields.
  .env.example is the complete runtime variable inventory.
- Old flat documentation paths remain navigation pages for existing links.
  Put substantive updates in the focused documents and run make docs-check.
