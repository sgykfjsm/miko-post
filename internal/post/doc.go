// Package post is the shared posting core. It validates a message, starts every
// enabled sink concurrently and independently, waits for all of them, and
// aggregates their results.
//
// Both front doors go through this package; neither duplicates validation,
// sink selection, submission, result aggregation, or logging.
package post
