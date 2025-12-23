package main

import (
	"log/slog"
	"net/http"
	"os"
	"strings"

	"pandapages/api/internal/db"
	"pandapages/api/internal/httpadmin"
	"pandapages/api/internal/httpapi"
)

func main() {
	store := db.MustOpen(os.Getenv("DATABASE_URL"))
	defer store.Close()

	pass := strings.TrimSpace(os.Getenv("PP_PASSCODE"))
	if pass == "" {
		panic("PP_PASSCODE is required")
	}

	adminKey := strings.TrimSpace(os.Getenv("PP_ADMIN_KEY"))
	// In dev you can allow empty; in prod you should require it.
	// If you want "always required", uncomment:
	// if adminKey == "" { panic("PP_ADMIN_KEY is required") }

	public := httpapi.New(httpapi.Config{
		Passcode:     pass,
		CookieSecure: os.Getenv("PP_COOKIE_SECURE") == "true",
		LogRequests:  os.Getenv("PP_LOG_LEVEL") == "debug",
	}, store)

	admin := httpadmin.New(httpadmin.Config{
		AdminKey:     adminKey,
		CookieSecure: os.Getenv("PP_COOKIE_SECURE") == "true",
		LogRequests:  os.Getenv("PP_LOG_LEVEL") == "debug",
	}, store)

	root := http.NewServeMux()
	root.Handle("/api/v1/admin/", admin)
	root.Handle("/", public)

	slog.Info("api listening", "addr", ":8080")
	_ = http.ListenAndServe(":8080", root)
}
