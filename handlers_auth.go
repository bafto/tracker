package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
)

// basicAuthMiddleware requires HTTP Basic Auth with the configured
// credentials; the browser caches them and sends them on every request.
// Hashes are compared in constant time so neither length nor content leaks
// via timing.
//
// Responses: 401 Unauthorized with a WWW-Authenticate challenge (missing or
// wrong credentials); otherwise calls next.
func basicAuthMiddleware(cfg Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if ok {
			userHash := sha256.Sum256([]byte(user))
			passHash := sha256.Sum256([]byte(pass))
			userOK := cfg.Username == "" || subtle.ConstantTimeCompare(userHash[:], cfg.usernameHash[:]) == 1
			passOK := subtle.ConstantTimeCompare(passHash[:], cfg.passwordHash[:]) == 1
			if userOK && passOK {
				next.ServeHTTP(w, r)
				return
			}
			getLogger(r.Context()).Warn("wrong basic auth credentials", "user", user)
		}

		w.Header().Set("WWW-Authenticate", `Basic realm="tracker", charset="UTF-8"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}
