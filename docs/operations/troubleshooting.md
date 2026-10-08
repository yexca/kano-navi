# Troubleshooting

| Symptom                                   | Checks and recovery                                                                                                                         |
| ----------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| Server refuses startup                    | Check APP_MODE and production ADMIN_PASSWORD length. Authentication is validated before SQLite opens.                                       |
| Fresh board has no posts or providers     | Expected initialization. Configure providers/routes in /admin and run a workflow.                                                           |
| Stale data after a failed run             | Inspect guarded sync history; the previous snapshot is retained. Check source coverage and request budgets.                                 |
| Date range returns partial coverage       | Check source credential-availability flags and pagination budgets. Public profile/RSS coverage cannot expand to a full archive.             |
| Timed workflow does not run               | Check WORKFLOW_SCHEDULER_ENABLED, next run, and the single-flight queue. The API process must remain running.                               |
| Schedule extraction skipped               | Check route ordering, keyed provider capabilities, stage flags, and ready cached images. Pending images are not reduced to text-only input. |
| Model provider cannot connect             | Inspect sanitized admin status. Private/loopback provider origins are rejected; redirects are blocked.                                      |
| Media returns 404                         | Verify a ready database row and a contained cache path. Missing bytes do not trigger an external browser fetch.                             |
| Admin session disappears                  | Sessions are in memory and expire on restart. Log in again.                                                                                 |
| HTTPS admin mutation rejected             | Preserve Sec-Fetch-Site and forwarded protocol; non-browser Origin checks need the public Host.                                             |
| Native SQLite dependency fails to install | Match the Node major used by Docker/CI and check the native compiler toolchain. Reinstall locked dependencies.                              |
| Development container uses old API code   | Restart the development Compose service. Rebuild and renew anonymous dependency volumes after lockfile changes.                             |

Use controlled error codes and synthetic reproduction records in public issues.
Never paste secrets, raw provider responses, databases, or deployment paths.

## Related Docs

- [Reliability](reliability.md)
- [Configuration](configuration.md)
- [Database operations](database.md)
- [Docker](docker.md)
- [Security](security.md)
