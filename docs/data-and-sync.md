# Data and Synchronization

- [Data model and seed behavior](architecture/data-model.md)
- [Sources and schedule extraction](architecture/sources.md)
- [Workflow scheduling](architecture/workflows.md)
- [Failure and freshness](operations/reliability.md)
- [Runtime configuration](operations/configuration.md)
- [Database and manual maintenance](operations/database.md)

## Schedule Images

The source-text gate and image-model/manual approval contract lives in
[Schedule images](architecture/sources.md#schedule-images).

## Fetch Windows and Historical Backfill

Rolling days, inclusive Japan dates, historical credentials, pagination,
checkpoint retention, and incremental-cursor isolation are documented in
[Fetch windows](architecture/workflows.md#fetch-windows-and-historical-backfill).

This page preserves existing links; update the focused contracts above.
