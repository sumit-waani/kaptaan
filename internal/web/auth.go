package web

import (
        "crypto/rand"
        "encoding/hex"
        "encoding/json"
        "io"
        "net/http"
        "time"

        "golang.org/x/crypto/bcrypt"
)

const (
        sessionCookieName = "kaptaan_session"
        sessionTTL        = 30 * 24 * time.Hour
        bcryptCost        = 12
)

// newToken generates a cryptographically random 32-byte hex token.
func newToken() (string, error) {
        b := make([]byte, 32)
        if _, err := rand.Read(b); err != nil {
                return "", err
        }
        return hex.EncodeToString(b), nil
}

// requireAuth is middleware that blocks unauthenticated requests with 401.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
                cookie, err := r.Cookie(sessionCookieName)
                if err != nil {
                        jsonErr(w, "unauthorized", http.StatusUnauthorized)
                        return
                }
                _, exp, err := s.db.GetSession(r.Context(), cookie.Value)
                if err != nil || time.Now().After(exp) {
                        jsonErr(w, "session expired", http.StatusUnauthorized)
                        return
                }
                next(w, r)
        }
}

// handleAuthStatus returns whether a user account exists and whether the
// current request carries a valid session.
func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
        hasUser := s.db.HasUser(r.Context())
        loggedIn := false
        if cookie, err := r.Cookie(sessionCookieName); err == nil {
                _, exp, err := s.db.GetSession(r.Context(), cookie.Value)
                loggedIn = err == nil && time.Now().Before(exp)
        }
        jsonOK(w, map[string]bool{"hasUser": hasUser, "loggedIn": loggedIn})
}

// handleAuthSetup creates the single user account. Fails if one already exists.
func (s *Server) handleAuthSetup(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "POST required", http.StatusMethodNotAllowed)
                return
        }
        if s.db.HasUser(r.Context()) {
                jsonErr(w, "account already exists", http.StatusConflict)
                return
        }

        var req struct {
                Username string `json:"username"`
                Password string `json:"password"`
        }
        body, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
        if err := json.Unmarshal(body, &req); err != nil {
                jsonErr(w, "invalid JSON", http.StatusBadRequest)
                return
        }
        if len(req.Username) < 1 {
                jsonErr(w, "username is required", http.StatusBadRequest)
                return
        }
        if len(req.Password) < 6 {
                jsonErr(w, "password must be at least 6 characters", http.StatusBadRequest)
                return
        }

        hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcryptCost)
        if err != nil {
                jsonErr(w, "internal error", http.StatusInternalServerError)
                return
        }
        if err := s.db.CreateUser(r.Context(), req.Username, string(hash)); err != nil {
                jsonErr(w, "could not create account", http.StatusInternalServerError)
                return
        }

        s.issueSession(w, r, req.Username)
}

// handleAuthLogin checks credentials and issues a session cookie.
func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "POST required", http.StatusMethodNotAllowed)
                return
        }

        var req struct {
                Username string `json:"username"`
                Password string `json:"password"`
        }
        body, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
        if err := json.Unmarshal(body, &req); err != nil {
                jsonErr(w, "invalid JSON", http.StatusBadRequest)
                return
        }

        hash, err := s.db.GetUserPasswordHash(r.Context(), req.Username)
        if err != nil {
                // Run bcrypt anyway to prevent timing-based user enumeration.
                _ = bcrypt.CompareHashAndPassword(
                        []byte("$2a$12$invalidhashpaddingthatmakescomparerunfull0000000000000"),
                        []byte(req.Password),
                )
                jsonErr(w, "invalid credentials", http.StatusUnauthorized)
                return
        }
        if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
                jsonErr(w, "invalid credentials", http.StatusUnauthorized)
                return
        }

        s.issueSession(w, r, req.Username)
}

// handleAuthLogout deletes the session and clears the cookie.
func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "POST required", http.StatusMethodNotAllowed)
                return
        }
        if cookie, err := r.Cookie(sessionCookieName); err == nil {
                _ = s.db.DeleteSession(r.Context(), cookie.Value)
        }
        http.SetCookie(w, &http.Cookie{
                Name: sessionCookieName, Value: "", MaxAge: -1,
                Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
        })
        jsonOK(w, map[string]string{"ok": "logged out"})
}

// issueSession creates a DB session record and sets the session cookie.
func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, username string) {
        token, err := newToken()
        if err != nil {
                jsonErr(w, "token error", http.StatusInternalServerError)
                return
        }
        exp := time.Now().Add(sessionTTL)
        if err := s.db.CreateSession(r.Context(), token, username, exp); err != nil {
                jsonErr(w, "session error", http.StatusInternalServerError)
                return
        }
        http.SetCookie(w, &http.Cookie{
                Name:     sessionCookieName,
                Value:    token,
                Expires:  exp,
                MaxAge:   int(sessionTTL.Seconds()),
                Path:     "/",
                HttpOnly: true,
                SameSite: http.SameSiteLaxMode,
        })
        jsonOK(w, map[string]string{"ok": "authenticated"})
}
