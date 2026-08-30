package main

import (
	"io"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"gova/app/cache"
	"gova/app/db"
	"gova/app/handlers"
	"gova/app/middleware"
)

func main() {
	if logPath := os.Getenv("LOG_PATH"); logPath != "" {
		if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
			log.SetOutput(io.MultiWriter(os.Stdout, f))
		}
	}

	if secret := os.Getenv("SESSION_SECRET"); len(secret) < 32 {
		log.Fatal("SESSION_SECRET must be set and at least 32 characters")
	}

	database, err := db.Open(os.Getenv("DB_PATH"))
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer database.Close()

	appCache := cache.New()
	_ = appCache

	r := chi.NewRouter()
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)
	r.Use(middleware.Security)
	r.Use(middleware.CSRF)
	r.Use(middleware.Auth)
	// Session revocation is wired by scaffold output: the generated auth
	// file's init() installs middleware.EpochLookup against the users table
	// (see templates/auth_handler.go.tmpl). An app scaffolded without auth
	// has neither sessions to revoke nor a lookup — EpochLookup stays nil
	// and the epoch check passes everything, the pre-epoch behavior.

	// Fallbacks for a path that matched no route, and for a method that is not
	// registered on a path that did. chi's defaults answer in plain text, which
	// breaks the envelope contract for /api/ callers; these answer in the
	// envelope there and leave human-facing URLs alone.
	r.NotFound(handlers.NotFoundHandler())
	r.MethodNotAllowed(handlers.MethodNotAllowedHandler())

	// Static files
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))

	// Pages. Source of truth: api.json's "pages" -> handlers/pages_gen.go.
	// Never hand-wire a page route here; create_page and the scaffold tools
	// regenerate RegisterPages. "/" is the framework's own home shell and is
	// not in the manifest.
	r.Get("/", handlers.HomeGET())
	handlers.RegisterPages(r)

	// API
	r.Get("/api/v1/_version", handlers.VersionGET())
	r.Get("/api/v1/_manifest", handlers.ManifestGET())

	// Generated API routes. Source of truth: api.json -> handlers/routes_gen.go.
	// Never hand-edit routes here; scaffold tools regenerate RegisterGenerated.
	handlers.RegisterGenerated(r, database, appCache)

	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("GOVA app listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
