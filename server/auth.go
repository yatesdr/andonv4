// auth.go — Session-based authentication with cookie management.
package server

import (
	"context"
	"crypto/rand"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Auth struct {
	Store     *Store
	Templates *template.Template
	mu        sync.Mutex
	sessions  map[string]time.Time
}

// NewAuth creates a session-based authentication handler.
func NewAuth(store *Store, tmpl *template.Template) *Auth {
	return &Auth{
		Store:     store,
		Templates: tmpl,
		sessions:  make(map[string]time.Time),
	}
}

func (a *Auth) IsLoggedIn(r *http.Request) bool {
	cookie, err := r.Cookie("session")
	if err != nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	exp, ok := a.sessions[cookie.Value]
	if !ok || time.Now().After(exp) {
		delete(a.sessions, cookie.Value)
		return false
	}
	return true
}

func (a *Auth) LoginPage(w http.ResponseWriter, r *http.Request) {
	data := struct {
		SimpleBarData
		Error bool
		Next  string
	}{
		SimpleBarData: SimpleBarData{Title: "Login"},
		Error:         r.URL.Query().Get("error") == "1",
		Next:          r.URL.Query().Get("next"),
	}
	if err := a.Templates.ExecuteTemplate(w, "login.html", data); err != nil {
		log.Printf("template error: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func (a *Auth) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	pass := r.FormValue("password")

	next := r.FormValue("next")

	settings := a.Store.GetSettings()
	if !CheckPassword(pass, settings.AdminPassHash) {
		errURL := "/login?error=1"
		if next != "" {
			errURL += "&next=" + url.QueryEscape(next)
		}
		http.Redirect(w, r, errURL, http.StatusSeeOther)
		return
	}

	// Migrate legacy SHA-256 hash to bcrypt on successful login
	if isLegacyHash(settings.AdminPassHash) {
		settings.AdminPassHash = HashPassword(pass)
		a.Store.SetSettings(settings)
	}

	token := generateToken()
	a.mu.Lock()
	a.sessions[token] = time.Now().Add(24 * time.Hour)
	a.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	if next == "" || next[0] != '/' || strings.HasPrefix(next, "//") {
		next = "/"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (a *Auth) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session")
	if err == nil {
		a.mu.Lock()
		delete(a.sessions, cookie.Value)
		a.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *Auth) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.IsLoggedIn(r) {
			a.unauthorized(w, r)
			return
		}
		next(w, r)
	}
}

// RequireAuthMiddleware is a chi-compatible middleware (func(http.Handler) http.Handler)
// for use with r.Use() in route groups.
func (a *Auth) RequireAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.IsLoggedIn(r) {
			a.unauthorized(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// unauthorized returns a 401 JSON response for API routes, or redirects to /login for pages.
func (a *Auth) unauthorized(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthorized"}`))
		return
	}
	http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
}

func (a *Auth) StartCleanup(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.mu.Lock()
				now := time.Now()
				for token, exp := range a.sessions {
					if now.After(exp) {
						delete(a.sessions, token)
					}
				}
				a.mu.Unlock()
			}
		}
	}()
}

func generateToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return fmt.Sprintf("%x", b)
}
