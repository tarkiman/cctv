package api

import (
	"context"
	"net/http"

	"github.com/tarkiman/cctv/nvr/server/internal/auth"
	"github.com/tarkiman/cctv/nvr/server/internal/store"
)

type grantsKey struct{}

// withGrants loads the signed-in user's camera permissions once per request. Admins
// always see every camera.
func (s *Server) withGrants(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g := &store.Grants{All: true}
		if id := auth.FromContext(r.Context()); id != nil && id.User.Role != "admin" {
			loaded, err := s.Store.Access.Get(r.Context(), id.User.ID)
			if err != nil {
				s.internalError(w, r, err)
				return
			}
			g = loaded
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), grantsKey{}, g)))
	})
}

func grantsFrom(r *http.Request) *store.Grants {
	if g, ok := r.Context().Value(grantsKey{}).(*store.Grants); ok {
		return g
	}
	return &store.Grants{} // fail closed if the middleware is missing
}

func canSee(r *http.Request, c *store.Camera) bool {
	return grantsFrom(r).Allows(c.ID, c.LocationID.Int64)
}

// visibleCameras returns the cameras the user may see.
func (s *Server) visibleCameras(r *http.Request) ([]store.Camera, error) {
	cams, err := s.Store.Cameras.List(r.Context())
	if err != nil {
		return nil, err
	}
	g := grantsFrom(r)
	if g.All {
		return cams, nil
	}
	out := cams[:0]
	for _, c := range cams {
		if g.Allows(c.ID, c.LocationID.Int64) {
			out = append(out, c)
		}
	}
	return out, nil
}

// visibleIDs is a set of visible camera IDs (nil means all).
func (s *Server) visibleIDs(r *http.Request) (map[int64]bool, error) {
	if grantsFrom(r).All {
		return nil, nil
	}
	cams, err := s.visibleCameras(r)
	if err != nil {
		return nil, err
	}
	ids := make(map[int64]bool, len(cams))
	for _, c := range cams {
		ids[c.ID] = true
	}
	return ids, nil
}

// canSeeID checks a camera by ID (for resources that only carry a camera ID).
func (s *Server) canSeeID(r *http.Request, cameraID int64) bool {
	if grantsFrom(r).All {
		return true
	}
	c, err := s.Store.Cameras.ByID(r.Context(), cameraID)
	return err == nil && canSee(r, c)
}

func isAdmin(r *http.Request) bool {
	id := auth.FromContext(r.Context())
	return id != nil && id.User.Role == "admin"
}
