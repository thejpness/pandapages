package httpadmin

import "pandapages/api/internal/httpbearer"

type Config struct {
	AdminKey            string
	BearerAuthenticator *httpbearer.Authenticator
}
