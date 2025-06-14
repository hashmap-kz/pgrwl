// Package opt provides optional integrations and components
// that are not part of the core system, but can be used to extend it.
//
// It includes the following subpackages:
//
//   - jobq: job queue and background task processing
//   - metrics: Prometheus metrics and observability helpers
//   - modes: runtime modes and configuration logic
//   - shared: internal shared code used by optional components
//   - supervisors: long-running background supervisors and orchestrators
//
// These components are modular and can be imported selectively.
package opt
