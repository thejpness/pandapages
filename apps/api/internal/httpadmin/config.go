package httpadmin

import "pandapages/api/internal/session"

type Config struct {
	AdminKey string
	Sessions *session.Manager
}
