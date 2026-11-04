package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/tarkiman/cctv/nvr/server/internal/auth"
)

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Format permintaan tidak valid.")
		return
	}
	u, err := s.Auth.Login(r.Context(), w, r, strings.TrimSpace(req.Username), req.Password)
	switch {
	case errors.Is(err, auth.ErrRateLimited):
		wait := s.Auth.RetryAfter(auth.ClientIP(r))
		w.Header().Set("Retry-After", fmt.Sprint(int(wait.Seconds())))
		writeError(w, http.StatusTooManyRequests, "rate_limited",
			fmt.Sprintf("Terlalu banyak percobaan. Coba lagi dalam %d detik.", int(wait.Seconds())))
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Username atau password salah.")
	case err != nil:
		s.internalError(w, r, err)
	default:
		writeJSON(w, http.StatusOK, u)
	}
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.Auth.Logout(r.Context(), w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, auth.FromContext(r.Context()).User)
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Format permintaan tidak valid.")
		return
	}
	if len(req.New) < 10 {
		writeError(w, http.StatusBadRequest, "weak_password", "Password baru minimal 10 karakter.")
		return
	}
	ident := auth.FromContext(r.Context())
	if _, err := s.Auth.Verify(r.Context(), auth.ClientIP(r), ident.User.Username, req.Current); err != nil {
		if errors.Is(err, auth.ErrRateLimited) {
			writeError(w, http.StatusTooManyRequests, "rate_limited", "Terlalu banyak percobaan. Coba lagi nanti.")
			return
		}
		writeError(w, http.StatusBadRequest, "wrong_password", "Password saat ini salah.")
		return
	}
	hash, err := auth.HashPassword(req.New)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.Store.Users.SetPassword(r.Context(), ident.User.ID, hash); err != nil {
		s.internalError(w, r, err)
		return
	}
	// Sign out every other device; this one stays logged in.
	if err := s.Store.Sessions.DeleteOthers(r.Context(), ident.User.ID, ident.SessionID); err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
