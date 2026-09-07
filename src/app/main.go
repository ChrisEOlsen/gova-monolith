package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

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
	// middleware.RequestLogger rather than chi's: chi's logs the full RequestURI,
	// which puts query strings — ?filter=email:someone@example.com — into a
	// host-mounted logfile. See middleware/logger.go.
	r.Use(middleware.RequestLogger)
	r.Use(chiMiddleware.Recoverer)
	r.Use(middleware.Security)
	r.Use(middleware.CSRF)
	r.Use(middleware.Auth(
		models.NewUserModel(database),
		models.NewMobileTokenModel(database),
	))

	// chi's own fallbacks answer in plain text, which breaks the envelope for
	// /api/ callers. Human-facing URLs keep the ordinary page response.
	r.NotFound(handlers.NotFoundHandler())
	r.MethodNotAllowed(handlers.MethodNotAllowedHandler())

	r.Handle("/static/*", http.StripPrefix("/static/", noDirIndex(http.FileServer(http.Dir("./static")))))

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

	// Every timeout is set explicitly. http.ListenAndServe leaves all four at
	// zero, which means "no deadline": a Slowloris client that opens a
	// connection and dribbles one header byte per minute holds a goroutine and
	// a file descriptor open forever, and enough of them are the whole outage.
	srv := &http.Server{
		Addr:    ":" + port,
		Handler: r,
		// Header deadline is separate from and shorter than the body deadline:
		// a slow upload on a fast link is legitimate, a slow header is not.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	log.Printf("GOVA app listening on :%s", port)
	log.Fatal(srv.ListenAndServe())
}

// noDirIndex turns http.FileServer's directory listing off. A request for
// /static/js/ would otherwise enumerate every module in the tree — not a
// vulnerability on its own, since everything under static/ is public by
// definition, but free reconnaissance for anyone probing the app.
func noDirIndex(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
