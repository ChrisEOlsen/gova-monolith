package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"gova/app/db"
	"gova/app/middleware"
	"gova/app/models"
)

const minPasswordLength = 8

// RegisterPOST handles POST /api/v1/auth/register
func RegisterPOST(database *db.DB) http.HandlerFunc {
	users := models.NewUserModel(database)
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name     string `json:"name"`
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		name := strings.TrimSpace(body.Name)
		email := strings.TrimSpace(body.Email)

		if fields := validateRegistration(name, email, body.Password); len(fields) > 0 {
			jsonValidationError(w, fields)
			return
		}

		// Metered because the 409 below tells the caller whether an address
		// already has an account — the fact login goes to some trouble to hide.
		// Every attempt counts, success included: account creation is the thing
		// being limited.
		bucket := registerBucket(clientIP(r))
		locked, err := users.IsRateLimited(bucket)
		if err != nil {
			log.Printf("handlers: rate-limit read failed for %q: %v", bucket, err)
			jsonError(w, "Something went wrong. Try again.", http.StatusInternalServerError)
			return
		}
		if locked {
			jsonError(w, "Too many attempts. Try again in 15 minutes.", http.StatusTooManyRequests)
			return
		}
		record(users, bucket, maxAttemptsPerIP)

		id, err := users.Create(name, email, body.Password)
		if err != nil {
			switch {
			case errors.Is(err, models.ErrDuplicateEmail):
				jsonError(w, "An account with that email already exists.", http.StatusConflict)
			case errors.Is(err, models.ErrPasswordTooLong):
				jsonValidationError(w, map[string]string{
					"password": "must be at most 72 bytes",
				})
			default:
				log.Printf("handlers: registration failed: %v", err)
				jsonError(w, "Registration failed. Please try again.", http.StatusInternalServerError)
			}
			return
		}

		middleware.SetSession(w, id, users.SessionEpoch(id))
		jsonOK(w, map[string]any{"id": id, "name": name, "email": email})
	}
}

func validateRegistration(name, email, password string) map[string]string {
	fields := map[string]string{}
	if name == "" {
		fields["name"] = "required"
	}
	if !strings.Contains(email, "@") {
		fields["email"] = "must be a valid email address"
	}
	switch {
	case len(password) < minPasswordLength:
		fields["password"] = "must be at least 8 characters"
	case len(password) > models.MaxPasswordBytes:
		fields["password"] = "must be at most 72 bytes"
	}
	return fields
}
