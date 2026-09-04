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
	"gova/app/models"
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

	r := chi.NewRouter()
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)
	r.Use(middleware.Security)
	r.Use(middleware.CSRF)
	r.Use(middleware.Auth(models.NewUserModel(database)))

	// chi's own fallbacks answer in plain text, which breaks the envelope for
	// /api/ callers. Human-facing URLs keep the ordinary page response.
	r.NotFound(handlers.NotFoundHandler())
	r.MethodNotAllowed(handlers.MethodNotAllowedHandler())

	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))

	// Home is the framework's own shell and is not in the manifest. Every other
	// page comes from api.json via pages_gen.go — never hand-wire one here.
	r.Get("/", handlers.HomeGET())
	handlers.RegisterPages(r)

	r.Get("/api/v1/_version", handlers.VersionGET())

	// API routes from api.json via routes_gen.go — never hand-wire one here.
	handlers.RegisterGenerated(r, database, appCache)

	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("GOVA app listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
