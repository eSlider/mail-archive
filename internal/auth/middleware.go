package auth

import (
	"context"
	"net/http"
)

type ctxKey string

const userIDKey ctxKey = "user_id"

// RequireAuth is middleware that checks for a valid session.
// Redirects to login page for HTML requests, returns 401 for API requests.
func RequireAuth(sessions *SessionStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := TokenFromRequest(r)
			if token == "" {
				unauthorized(w, r)
				return
			}

			sess := sessions.Get(token)
			if sess == nil {
				ClearCookie(w)
				unauthorized(w, r)
				return
			}

			ctx := context.WithValue(r.Context(), userIDKey, sess.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserIDFromContext returns the authenticated user ID, or empty string.
func UserIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(userIDKey).(string); ok {
		return id
	}
	return ""
}

// OptionalAuth is middleware that loads the user if a session exists,
// but does not block the request if there is none.
func OptionalAuth(sessions *SessionStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := TokenFromRequest(r)
			if token != "" {
				if sess := sessions.Get(token); sess != nil {
					ctx := context.WithValue(r.Context(), userIDKey, sess.UserID)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func unauthorized(w http.ResponseWriter, r *http.Request) {
	// API requests get JSON 401; browser requests get redirected.
	if r.Header.Get("Accept") == "application/json" ||
		len(r.URL.Path) >= 4 && r.URL.Path[:4] == "/api" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthorized"}`))
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
