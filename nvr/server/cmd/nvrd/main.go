// Command nvrd is the NVR Lite server: it records cameras through go2rtc, serves the web
// app and API, and keeps recordings within their retention limits.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"syscall"
	"time"
	_ "time/tzdata" // the runtime image may lack zoneinfo

	"github.com/jmoiron/sqlx"

	"github.com/tarkiman/cctv/nvr/server/internal/api"
	"github.com/tarkiman/cctv/nvr/server/internal/auth"
	"github.com/tarkiman/cctv/nvr/server/internal/camera"
	"github.com/tarkiman/cctv/nvr/server/internal/config"
	"github.com/tarkiman/cctv/nvr/server/internal/db"
	"github.com/tarkiman/cctv/nvr/server/internal/ffmpeg"
	"github.com/tarkiman/cctv/nvr/server/internal/go2rtc"
	"github.com/tarkiman/cctv/nvr/server/internal/health"
	"github.com/tarkiman/cctv/nvr/server/internal/notify"
	"github.com/tarkiman/cctv/nvr/server/internal/recorder"
	"github.com/tarkiman/cctv/nvr/server/internal/retention"
	"github.com/tarkiman/cctv/nvr/server/internal/snapshot"
	"github.com/tarkiman/cctv/nvr/server/internal/sse"
	"github.com/tarkiman/cctv/nvr/server/internal/store"
	"github.com/tarkiman/cctv/nvr/server/internal/sysinfo"
	"github.com/tarkiman/cctv/nvr/server/internal/timelapse"
	"github.com/tarkiman/cctv/nvr/server/web"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("nvrd stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)
	log.Info("starting nvrd", "version", version, "data_dir", cfg.DataDir, "listen", cfg.Listen)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return err
	}
	database, err := db.Open(ctx, filepath.Join(cfg.DataDir, "nvr.db"))
	if err != nil {
		return err
	}
	defer database.Close()
	st := store.New(database)

	authSvc := auth.New(st, cfg.SecureCookie)
	created, err := authSvc.Bootstrap(ctx, cfg.AdminUser, cfg.AdminPassword)
	if err != nil {
		return err
	}
	if created {
		log.Info("created first admin user", "username", cfg.AdminUser)
	} else if n, _ := st.Users.Count(ctx); n == 0 {
		log.Warn("no users exist; set NVR_ADMIN_USER and NVR_ADMIN_PASSWORD to create the first admin")
	}

	hub := sse.NewHub()
	tracker := health.New(hub)
	g2r := go2rtc.New(cfg.Go2rtcURL)
	tools := ffmpeg.Tools{FFmpeg: cfg.FFmpeg, FFprobe: cfg.FFprobe, RTSP: cfg.Go2rtcRTSP}

	rec := &recorder.Recorder{
		Tools: tools, Store: st, Health: tracker, DataDir: cfg.DataDir,
		Location: cfg.Location, SegmentSeconds: cfg.SegmentSeconds, Log: log.With("component", "recorder"),
	}
	rec.Start(ctx)
	snap := &snapshot.Snapshotter{
		Tools: tools, Health: tracker, DataDir: cfg.DataDir, Location: cfg.Location,
		Every: cfg.SnapshotEvery, Log: log.With("component", "snapshot"),
	}
	mgr := &camera.Manager{
		Store: st, Go2rtc: g2r, Health: tracker, Recorder: rec, Snapshotter: snap,
		Log: log.With("component", "cameras"),
	}
	mgr.Start(ctx)

	go (&timelapse.Compiler{Tools: tools, Store: st, DataDir: cfg.DataDir, Location: cfg.Location,
		Log: log.With("component", "timelapse")}).Run(ctx)
	go (&retention.Janitor{Store: st, Hub: hub, DataDir: cfg.DataDir,
		Log: log.With("component", "retention")}).Run(ctx)
	go housekeeping(ctx, st, database, cfg, log)

	notifier := notify.New(cfg.NotifyWebhooks, log.With("component", "notify"))
	go (&notify.Watcher{
		Notifier: notifier, Store: st, Health: tracker, PublicURL: cfg.PublicURL,
		Disk:         func() (uint64, uint64) { return sysinfo.Disk(cfg.DataDir) },
		OfflineAfter: cfg.NotifyOfflineAfter, Interval: 30 * time.Second,
		Log: log.With("component", "notify"),
	}).Run(ctx)
	if notifier.Enabled() {
		// Confirms that the box came back after a reboot or deploy.
		go notifier.Send(ctx, notify.Message{
			Kind: "startup", Level: notify.Info, Title: "NVR Lite aktif",
			Text: "nvrd " + version + " berjalan.", URL: cfg.PublicURL,
		})
	}

	srv := &http.Server{
		Addr: cfg.Listen,
		Handler: (&api.Server{
			Config: cfg, Store: st, Auth: authSvc, Go2rtc: g2r, Health: tracker, Cameras: mgr,
			Hub: hub, Sys: &sysinfo.Sampler{}, Notifier: notifier, Web: web.FS(), Version: version, Log: log,
		}).Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	// Recorders receive SIGINT through ctx and close their current segment; the next
	// start indexes it from .incoming.
	done := make(chan struct{})
	go func() { mgr.Wait(); close(done) }()
	select {
	case <-done:
	case <-shutdownCtx.Done():
		log.Warn("workers did not stop in time")
	}
	return nil
}

// housekeeping removes expired sessions and keeps a daily database backup (7 kept).
func housekeeping(ctx context.Context, st *store.Store, database *sqlx.DB, cfg *config.Config, log *slog.Logger) {
	dir := filepath.Join(cfg.DataDir, "backup")
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		if err := st.Sessions.DeleteExpired(ctx); err != nil && ctx.Err() == nil {
			log.Warn("delete expired sessions", "err", err)
		}
		name := filepath.Join(dir, "nvr-"+time.Now().In(cfg.Location).Format("2006-01-02")+".db")
		if _, err := os.Stat(name); errors.Is(err, os.ErrNotExist) {
			if err := os.MkdirAll(dir, 0o750); err == nil {
				if err := db.Backup(ctx, database, name); err != nil {
					log.Warn("database backup", "err", err)
				}
			}
			if old, _ := filepath.Glob(filepath.Join(dir, "nvr-????-??-??.db")); len(old) > 7 {
				sort.Strings(old)
				for _, f := range old[:len(old)-7] {
					os.Remove(f)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
