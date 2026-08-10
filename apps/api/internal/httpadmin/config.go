package httpadmin

import (
	"pandapages/api/internal/httpbearer"
	"pandapages/api/internal/sourceprovider"
)

type Config struct {
	AdminKey            string
	BearerAuthenticator *httpbearer.Authenticator
	SourceDiscovery     sourceprovider.Discovery
	SourceAcquisition   sourceprovider.Acquisition
}
