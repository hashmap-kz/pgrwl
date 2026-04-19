// Package opt provides optional integrations and components
// that are not part of the core system, but can be used to extend it.
//
// It includes the following subpackages:
//
//   - api: entry point (REST API, long-running background supervisors and orchestrators)
//   - basebackup: streaming backup logic
//   - jobq: job queue and background task processing
//   - metrics: Prometheus metrics and observability helpers
//   - shared: internal shared code used by optional components
//
// These components are modular and can be imported selectively.
package opt
