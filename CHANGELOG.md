# Changelog

This changelog summarizes the latest 30 GitHub releases. Fully generated release notes are available from each linked release.

## [v1.0.36](https://github.com/pgrwl/pgrwl/releases/tag/v1.0.36) - 2026-05-13

- Multipart logic improved (s3)
- Integration tests for k8s, storage layout, related workflows
- CLI simplified
- API compatibility workflow (relimpact)

## [v1.0.35](https://github.com/pgrwl/pgrwl/releases/tag/v1.0.35) - 2026-05-03

- Added backup supervisor unit tests and local development integration tests.
- Refactored the basebackup API and `all-in-one` mode internals.
- Updated local development, Kubernetes manifests, Grafana dashboards, and README assets.

## [v1.0.34](https://github.com/pgrwl/pgrwl/releases/tag/v1.0.34) - 2026-05-01

- Added `all-in-one` mode, manual basebackup API support, and `recovery-window` retention.
- Reworked serve/receive/backup error handling and shutdown behavior.
- Updated examples, docs, and integration test coverage around the new design.

## [v1.0.33](https://github.com/pgrwl/pgrwl/releases/tag/v1.0.33) - 2026-04-26

- Added UI (v3) with configuration defaults and dashboard config documentation.
- Fixed UI layout, WAL file extension handling, and screenshot assets.
- Prepared UI release/build automation updates.

## [v1.0.32](https://github.com/pgrwl/pgrwl/releases/tag/v1.0.32) - 2026-04-25

- Added REST API endpoints for listing backups/WALs and returning redacted config.
- Added basebackup timing information and retry behavior.
- Updated Docker Compose quick-start docs and dependency versions.

## [v1.0.31](https://github.com/pgrwl/pgrwl/releases/tag/v1.0.31) - 2026-04-18

- Refactored toward receive-mode-only operation.
- Improved multipart upload configurability and dashboard/config examples.
- Updated S3 SDK dependencies.

## [v1.0.30](https://github.com/pgrwl/pgrwl/releases/tag/v1.0.30) - 2026-04-16

- Added basebackup trace logs with cluster size and ETA reporting.
- Expanded integration tests for unavailable/flapping remote storage.
- Updated basebackup logs, workflows, and README content.

## [v1.0.29](https://github.com/pgrwl/pgrwl/releases/tag/v1.0.29) - 2026-04-09

- Added storage operation logging and embedded stream encryption work.
- Expanded integration flows, timing parity tests, and basebackup manifest work.
- Moved Helm charts to an external repo and updated storage tracing.

## [v1.0.28](https://github.com/pgrwl/pgrwl/releases/tag/v1.0.28) - 2026-03-28

- Transferred the project to the organization namespace.
- Updated Helm chart versioning and related documentation.

## [v1.0.27](https://github.com/pgrwl/pgrwl/releases/tag/v1.0.27) - 2026-03-28

- Added multipart S3 uploader support.
- Added integration tests for multipart S3 uploads, remote-only WAL restore, and storage behavior.
- Updated Go, chart, storage, and release workflow dependencies.

## [v1.0.26](https://github.com/pgrwl/pgrwl/releases/tag/v1.0.26) - 2025-12-04

- Expanded integration tests for tablespaces, streaming files, dynamic storage, and scheduled runs.
- Improved test isolation, parallel execution, and debugging scripts.
- Continued basebackup module cleanup and restore-tablespace fixes.

## [v1.0.25](https://github.com/pgrwl/pgrwl/releases/tag/v1.0.25) - 2025-11-23

- Added PostgreSQL 18 and multi-version integration test support.
- Improved integration test caching and image tagging.
- Updated chart versioning and CI restore rules.

## [v1.0.24](https://github.com/pgrwl/pgrwl/releases/tag/v1.0.24) - 2025-11-22

- Added top-level directory listing support.
- Improved PostgreSQL environment variable validation.
- Updated core dependencies, CI cleanup, and integration test stability.

## [v1.0.23](https://github.com/pgrwl/pgrwl/releases/tag/v1.0.23) - 2025-07-04

- Added environment-based config loading.
- Expanded environment variable configuration reference and quick-start docs.
- Updated backup/env config validation and Helm chart examples.

## [v1.0.22](https://github.com/pgrwl/pgrwl/releases/tag/v1.0.22) - 2025-07-02

- Added the initial Helm chart implementation.

## [v1.0.21](https://github.com/pgrwl/pgrwl/releases/tag/v1.0.21) - 2025-06-29

- Added basebackup metrics.
- Updated WAL deletion metrics and backup dashboards.
- Reorganized package structure and Kubernetes/example observability configs.

## [v1.0.20](https://github.com/pgrwl/pgrwl/releases/tag/v1.0.20) - 2025-06-18

- Added receiver brief-config API and basebackup manifest support.
- Added basebackup retention settings with keep-last and POSIX cron support.
- Improved WAL archive cleanup tied to basebackup retention.

## [v1.0.19](https://github.com/pgrwl/pgrwl/releases/tag/v1.0.19) - 2025-06-15

- Added time/count-based basebackup retention.
- Refactored shared option components and storage manifest placement.
- Updated Kubernetes manifests, config reference, and disaster recovery docs.

## [v1.0.18](https://github.com/pgrwl/pgrwl/releases/tag/v1.0.18) - 2025-06-13

- Added basebackup done-marker upload and basebackup retention scaffolding.
- Reworked config ownership around receiver uploader/retention settings.
- Added backup-mode and serve-mode diagrams plus basebackup config docs.

## [v1.0.17](https://github.com/pgrwl/pgrwl/releases/tag/v1.0.17) - 2025-06-10

- Added basebackup CLI implementation.
- Added restore-from-latest-backup behavior.
- Updated restore integration coverage and developer notes.

## [v1.0.16](https://github.com/pgrwl/pgrwl/releases/tag/v1.0.16) - 2025-06-05

- Added basebackup CLI, restore command, and streaming component work.
- Fixed basebackup directory, labels, and internal tests.
- Updated README coverage for basebackup workflows.

## [v1.0.15](https://github.com/pgrwl/pgrwl/releases/tag/v1.0.15) - 2025-06-04

- Added basic PR review and welcome automation.
- Added in-memory storage implementation for testing.

## [v1.0.14](https://github.com/pgrwl/pgrwl/releases/tag/v1.0.14) - 2025-06-03

- Added GitHub Actions path filters and workflow dispatch support for integration tests.
- Moved the job queue into optional components.
- Added the docs directory and revised integration test automation.

## [v1.0.13](https://github.com/pgrwl/pgrwl/releases/tag/v1.0.13) - 2025-06-03

- Added structured changelog generation in GoReleaser.
- Added contributing documentation for commit conventions.
- Improved integration test workflow caching and Docker build context handling.

## [v1.0.12](https://github.com/pgrwl/pgrwl/releases/tag/v1.0.12) - 2025-06-02

- Added uptime metric support.
- Added config validation command support.
- Updated dashboards, integration tests, and build cache behavior.

## [v1.0.11](https://github.com/pgrwl/pgrwl/releases/tag/v1.0.11) - 2025-06-01

- Added the version flag.
- Added Dependabot auto-merge setup and PR welcome automation.
- Updated contributing docs, syntax highlighting, and CI cache behavior.
