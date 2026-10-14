package api

import (
	"net/http"
	"net/url"
	"strings"

	"magitrickle/api/utils"
)

// CORS alone does not stop simple cross-origin POST requests from reaching the
// API, including when authentication is disabled. Local CLI clients send neither
// Origin nor Sec-Fetch-Site and remain supported.
func browserProtection(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			allowed := r.Header.Get("Sec-Fetch-Site") != "cross-site"
			if origins, present := r.Header["Origin"]; present {
				if len(origins) != 1 {
					allowed = false
				} else {
					origin, err := url.Parse(origins[0])
					allowed = allowed && err == nil && origin != nil &&
						(origin.Scheme == "http" || origin.Scheme == "https") &&
						origin.User == nil && origin.Path == "" && origin.RawQuery == "" &&
						!origin.ForceQuery && origin.Fragment == "" &&
						strings.EqualFold(origin.Host, r.Host)
				}
			}
			if !allowed {
				utils.WriteError(w, http.StatusForbidden, "cross-origin API request rejected")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
