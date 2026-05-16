package handlers

import (
	"net/http"
)

func HomeGET() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./static/pages/home.html")
	}
}
