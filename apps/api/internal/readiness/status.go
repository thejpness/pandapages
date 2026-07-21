// Package readiness defines the small provider-neutral result contract shared
// by the database probe and public HTTP readiness endpoint.
package readiness

import "errors"

var (
	ErrDatabaseUnavailable = errors.New("database unavailable")
	ErrSchemaNotReady      = errors.New("schema not ready")
)
