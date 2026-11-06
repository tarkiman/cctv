// Package api is nvrd's HTTP layer: JSON API under /api/v1, live-view proxy to go2rtc,
// SSE, recording files, and the embedded web app.
package api

import (
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/tarkiman/cctv/nvr/server/internal/auth"
	"github.com/tarkiman/cctv/nvr/server/internal/camera"
	"github.com/tarkiman/cctv/nvr/server/internal/config"
	"github.com/tarkiman/cctv/nvr/server/internal/export"
	"github.com/tarkiman/cctv/nvr/server/internal/ffmpeg"
	"github.com/tarkiman/cctv/nvr/server/internal/go2rtc"
	"github.com/tarkiman/cctv/nvr/server/internal/health"
	"github.com/tarkiman/cctv/nvr/server/internal/notify"
	"github.com/tarkiman/cctv/nvr/server/internal/sse"
	"github.com/tarkiman/cctv/nvr/server/internal/store"
	"github.com/tarkiman/cctv/nvr/server/internal/sysinfo"
)

type Server struct {
	Config  *config.Config
	Store   *store.Store
	Auth    *auth.Service
	Go2rtc  *go2rtc.Client
	Health  *health.Tracker
	Cameras *camera.Manager
	Hub     *sse.Hub
	Sys     *sysinfo.Sampler
	Notify  *notify.Watcher
	Export  *export.Exporter
	Tools   ffmpeg.Tools
	Web     fs.FS
	Version string
	Log     *slog.Logger
}

func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(s.requestLog)
	r.Use(securityHeaders)

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(auth.CSRF(func(w http.ResponseWriter, r *http.Request) {
			writeError(w, http.StatusForbidden, "csrf", "Permintaan ditolak (header CSRF tidak ada).")
		}))
		r.Get("/healthz", s.healthz)
		r.With(middleware.Timeout(15*time.Second)).Post("/auth/login", s.login)

		r.Group(func(r chi.Router) {
			r.Use(s.Auth.Require(func(w http.ResponseWriter, r *http.Request) {
				writeError(w, http.StatusUnauthorized, "unauthenticated", "Silakan masuk terlebih dahulu.")
			}))
			r.Use(s.withGrants)

			// Long-lived responses: no timeout middleware.
			r.Get("/stream", s.stream)
			r.Get("/live/ws", s.liveWS)
			r.Get("/recordings/{id}/file", s.recordingFile)
			r.Get("/cameras/{id}/export", s.exportClip)
			r.Get("/timelapse/{cameraID}/{date}.mp4", s.timelapseFile)

			r.Group(func(r chi.Router) {
				r.Use(middleware.Timeout(30 * time.Second))
				r.Post("/auth/logout", s.logout)
				r.Get("/auth/me", s.me)
				r.Put("/auth/password", s.changePassword)
				r.Get("/auth/sessions", s.listSessions)
				r.Delete("/auth/sessions/{id}", s.deleteSession)
				r.Post("/auth/sessions/logout-others", s.deleteOtherSessions)

				r.Get("/cameras", s.listCameras)
				r.Get("/cameras/{id}", s.getCamera)
				r.Get("/cameras/{id}/snapshot.jpg", s.snapshot)
				r.Get("/cameras/{id}/timeline", s.timeline)
				r.Get("/cameras/{id}/recordings", s.listRecordings)
				r.Get("/cameras/{id}/recordings/at", s.recordingAt)
				r.Get("/cameras/{id}/export/estimate", s.exportEstimate)
				r.Get("/cameras/{id}/events", s.listEvents)
				r.Get("/recordings/{id}/next", s.recordingNext)
				r.Get("/timelapse", s.listTimelapse)
				r.Post("/live/webrtc", s.liveWebRTC)

				r.Get("/system/stats", s.systemStats)
				r.Get("/locations", s.listLocations)

				r.Group(func(r chi.Router) {
					r.Use(requireAdmin)
					r.Patch("/cameras/{id}", s.patchCamera)
					r.Post("/cameras/sync", s.syncCameras)
					r.Post("/cameras", s.createCamera)
					r.Post("/cameras/probe", s.probeCamera)
					r.Post("/cameras/discover", s.discoverCameras)
					r.Post("/cameras/discover/streams", s.discoverStreams)
					r.Post("/cameras/discover/link", s.linkDevice)
					r.Post("/cameras/discover/ignore", s.ignoreDevice)
					r.Get("/cameras/{id}/sources", s.cameraSources)
					r.Put("/cameras/{id}/sources", s.putCameraSources)
					r.Delete("/cameras/{id}", s.deleteCamera)
					r.Get("/system/status", s.systemStatus)
					r.Get("/settings", s.getSettings)
					r.Put("/settings", s.putSettings)
					r.Post("/system/notify-test", s.notifyTest)
					r.Get("/users", s.listUsers)
					r.Post("/users", s.createUser)
					r.Patch("/users/{id}", s.updateUser)
					r.Delete("/users/{id}", s.deleteUser)
					r.Get("/users/{id}/access", s.getUserAccess)
					r.Put("/users/{id}/access", s.putUserAccess)
					r.Post("/locations", s.createLocation)
					r.Patch("/locations/{id}", s.renameLocation)
					r.Delete("/locations/{id}", s.deleteLocation)
				})
			})
		})

		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			writeError(w, http.StatusNotFound, "not_found", "Endpoint tidak ditemukan.")
		})
	})

	r.Handle("/*", spaHandler(s.Web))
	return r
}

func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := auth.FromContext(r.Context()); id == nil || id.User.Role != "admin" {
			writeError(w, http.StatusForbidden, "forbidden", "Hanya admin yang boleh melakukan ini.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; media-src 'self' blob:; "+
			"connect-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; worker-src 'self'; "+
			"manifest-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		next.ServeHTTP(ww, r)
		// Media and polling endpoints would flood the log at info level.
		level := slog.LevelDebug
		if ww.Status() >= 500 {
			level = slog.LevelError
		} else if ww.Status() >= 400 && ww.Status() != http.StatusUnauthorized {
			level = slog.LevelWarn
		}
		s.Log.Log(r.Context(), level, "http", "method", r.Method, "path", r.URL.Path,
			"status", ww.Status(), "ms", time.Since(start).Milliseconds())
	})
}
