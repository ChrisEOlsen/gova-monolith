// Code generated from api.json by gova-builder. DO NOT EDIT.
package handlers

import (
	"github.com/go-chi/chi/v5"
	"gova/app/cache"
	"gova/app/db"
	"gova/app/middleware"
)

// RegisterGenerated mounts every API route in api.json. main.go calls this once
// and is never hand-edited for routes.
func RegisterGenerated(r chi.Router, database *db.DB, appCache *cache.Cache) {
	r.Post("/api/v1/auth/login", LoginPOST(database))
	r.Post("/api/v1/auth/login_token", MobileLoginPOST(database))
	r.Post("/api/v1/auth/logout", LogoutPOST())
	r.With(middleware.RequireAuth).Post("/api/v1/auth/logout_all", LogoutAllPOST(database))
	r.Delete("/api/v1/auth/logout_token", MobileLogoutDELETE(database))
	r.With(middleware.RequireAuth).Get("/api/v1/auth/me", MeGET(database))
	r.Get("/api/v1/auth/me_token", MobileMeGET(database))
	r.Post("/api/v1/auth/register", RegisterPOST(database))
}
