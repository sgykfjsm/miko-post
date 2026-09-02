// Package logging writes line-delimited JSON diagnostics with stable event
// names and a per-post correlation identifier, rotating the active log by size
// or age without ever deleting a rotated file.
package logging
