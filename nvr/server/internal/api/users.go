package api

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/tarkiman/cctv/nvr/server/internal/auth"
	"github.com/tarkiman/cctv/nvr/server/internal/store"
)

const minPassword = 10

var usernameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,31}$`)

type userDTO struct {
	ID       int64      `json:"id"`
	Username string     `json:"username"`
	Role     string     `json:"role"`
	Sessions int        `json:"sessions"`
	LastSeen *time.Time `json:"last_seen"`
	Created  time.Time  `json:"created_at"`
}

func validRole(r string) bool { return r == "admin" || r == "viewer" }

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.Store.Users.List(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]userDTO, 0, len(users))
	for _, u := range users {
		d := userDTO{ID: u.ID, Username: u.Username, Role: u.Role, Sessions: u.Sessions, Created: unixTime(u.CreatedAt)}
		if u.LastSeen > 0 {
			t := unixTime(u.LastSeen)
			d.LastSeen = &t
		}
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Format permintaan tidak valid.")
		return
	}
	req.Username = strings.ToLower(strings.TrimSpace(req.Username))
	switch {
	case !usernameRe.MatchString(req.Username):
		writeError(w, http.StatusBadRequest, "invalid_username", "Username 3–32 karakter: huruf kecil, angka, titik, minus, atau garis bawah.")
		return
	case len(req.Password) < minPassword:
		writeError(w, http.StatusBadRequest, "weak_password", "Password minimal 10 karakter.")
		return
	case !validRole(req.Role):
		writeError(w, http.StatusBadRequest, "invalid_role", "Peran harus admin atau viewer.")
		return
	}
	if _, err := s.Store.Users.ByUsername(r.Context(), req.Username); err == nil {
		writeError(w, http.StatusConflict, "username_taken", "Username sudah dipakai.")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if _, err := s.Store.Users.Create(r.Context(), req.Username, hash, req.Role); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.Log.Info("user created", "username", req.Username, "role", req.Role, "by", auth.FromContext(r.Context()).User.Username)
	s.listUsers(w, r)
}

// updateUser changes a user's role and/or resets their password. A reset signs the user
// out everywhere; the last admin cannot be demoted.
func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	u, ok := s.userFromPath(w, r)
	if !ok {
		return
	}
	var req struct {
		Role     *string `json:"role"`
		Password *string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Format permintaan tidak valid.")
		return
	}
	me := auth.FromContext(r.Context()).User
	if req.Role != nil && *req.Role != u.Role {
		if !validRole(*req.Role) {
			writeError(w, http.StatusBadRequest, "invalid_role", "Peran harus admin atau viewer.")
			return
		}
		if u.ID == me.ID {
			writeError(w, http.StatusBadRequest, "own_role", "Anda tidak bisa mengubah peran akun sendiri.")
			return
		}
		if u.Role == "admin" && s.lastAdmin(w, r) {
			return
		}
		if err := s.Store.Users.SetRole(r.Context(), u.ID, *req.Role); err != nil {
			s.internalError(w, r, err)
			return
		}
	}
	if req.Password != nil {
		if len(*req.Password) < minPassword {
			writeError(w, http.StatusBadRequest, "weak_password", "Password minimal 10 karakter.")
			return
		}
		if u.ID == me.ID {
			writeError(w, http.StatusBadRequest, "own_password", "Ganti password sendiri lewat bagian Akun.")
			return
		}
		hash, err := auth.HashPassword(*req.Password)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		if err := s.Store.Users.SetPassword(r.Context(), u.ID, hash); err != nil {
			s.internalError(w, r, err)
			return
		}
		if err := s.Store.Sessions.DeleteAllForUser(r.Context(), u.ID); err != nil {
			s.internalError(w, r, err)
			return
		}
	}
	s.Log.Info("user updated", "username", u.Username, "role_changed", req.Role != nil, "password_reset", req.Password != nil, "by", me.Username)
	s.listUsers(w, r)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	u, ok := s.userFromPath(w, r)
	if !ok {
		return
	}
	me := auth.FromContext(r.Context()).User
	if u.ID == me.ID {
		writeError(w, http.StatusBadRequest, "delete_self", "Anda tidak bisa menghapus akun sendiri.")
		return
	}
	if u.Role == "admin" && s.lastAdmin(w, r) {
		return
	}
	if err := s.Store.Users.Delete(r.Context(), u.ID); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.Log.Info("user deleted", "username", u.Username, "by", me.Username)
	s.listUsers(w, r)
}

func (s *Server) userFromPath(w http.ResponseWriter, r *http.Request) (*store.User, bool) {
	id, ok := idParam(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "user_not_found", "Pengguna tidak ditemukan.")
		return nil, false
	}
	u, err := s.Store.Users.ByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "user_not_found", "Pengguna tidak ditemukan.")
		return nil, false
	}
	if err != nil {
		s.internalError(w, r, err)
		return nil, false
	}
	return u, true
}

// lastAdmin writes an error and reports true when removing one admin would leave none.
func (s *Server) lastAdmin(w http.ResponseWriter, r *http.Request) bool {
	n, err := s.Store.Users.CountAdmins(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return true
	}
	if n <= 1 {
		writeError(w, http.StatusBadRequest, "last_admin", "Harus ada minimal satu admin.")
		return true
	}
	return false
}

// --- own devices (sessions) ---

type deviceDTO struct {
	ID        string    `json:"id"`
	Current   bool      `json:"current"`
	UserAgent string    `json:"user_agent"`
	IP        string    `json:"ip"`
	Created   time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen"`
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	ident := auth.FromContext(r.Context())
	devs, err := s.Store.Sessions.ListByUser(r.Context(), ident.User.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]deviceDTO, 0, len(devs))
	for _, d := range devs {
		out = append(out, deviceDTO{ID: d.ID, Current: d.ID == ident.SessionID, UserAgent: d.UserAgent, IP: d.IP,
			Created: unixTime(d.CreatedAt), LastSeen: unixTime(d.LastSeen)})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	ident := auth.FromContext(r.Context())
	id := chi.URLParam(r, "id")
	if id == ident.SessionID {
		writeError(w, http.StatusBadRequest, "current_session", "Untuk perangkat ini gunakan tombol Keluar.")
		return
	}
	if err := s.Store.Sessions.DeleteForUser(r.Context(), ident.User.ID, id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "session_not_found", "Sesi tidak ditemukan.")
		return
	} else if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteOtherSessions(w http.ResponseWriter, r *http.Request) {
	ident := auth.FromContext(r.Context())
	if err := s.Store.Sessions.DeleteOthers(r.Context(), ident.User.ID, ident.SessionID); err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
