package httpadmin

import "pandapages/api/internal/session"

type Config struct {
	AdminKey    string
	LogRequests bool
	Sessions    *session.Manager
}
