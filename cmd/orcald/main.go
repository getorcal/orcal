package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/getorcal/orcal/internal/api"
	"github.com/getorcal/orcal/internal/auth"
	"github.com/getorcal/orcal/internal/config"
	"github.com/getorcal/orcal/internal/exec"
	"github.com/getorcal/orcal/internal/runtime/docker"
	"github.com/getorcal/orcal/internal/sandbox"
	"github.com/getorcal/orcal/internal/snapshot"
	"github.com/getorcal/orcal/internal/store/sqlite"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "orcald:", err)
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	store, err := sqlite.Open(filepath.Join(cfg.DataDir, "orcal.db"))
	if err != nil {
		return err
	}
	defer store.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	rt, err := docker.New(cfg.DockerHost)
	if err != nil {
		return err
	}
	if err := rt.EnsureNetwork(ctx, cfg.NetworkName); err != nil {
		return fmt.Errorf("ensure network %s: %w", cfg.NetworkName, err)
	}

	quotaOK, err := rt.DiskQuotaSupported(ctx)
	if err != nil {
		logger.Warn("could not determine disk quota support", slog.String("error", err.Error()))
	} else if !quotaOK {
		logger.Warn("disk usage is not limited on this host: overlay2 on xfs with project quotas is required; a sandbox can fill the host disk")
	}

	token, generated, err := auth.Ensure(ctx, store.Settings(), cfg.Token)
	if err != nil {
		return err
	}
	if generated {
		fmt.Fprintf(os.Stdout, "orcal: generated API token (shown once): %s\n", token)
	}
	tokenHash, found, err := store.Settings().Get(ctx, auth.SettingKey)
	if err != nil {
		return fmt.Errorf("read token hash: %w", err)
	}
	if !found {
		return errors.New("no auth token hash persisted after Ensure")
	}

	defaults := sandbox.Resources{
		CPUMillis:   cfg.DefaultCPUMillis,
		MemoryBytes: cfg.DefaultMemoryBytes,
		PidsLimit:   cfg.DefaultPidsLimit,
	}
	sandboxes := sandbox.NewService(store.Sandboxes(), rt, defaults, cfg.NetworkName)
	execs, err := exec.NewService(store.Execs(), sandboxes, rt, filepath.Join(cfg.DataDir, "execs"), cfg.ExecOutputMaxBytes)
	if err != nil {
		return err
	}
	// Must run before exec reconciliation: a container left paused by a crashed daemon would
	// otherwise be treated as healthy and its execs polled forever.
	if n, err := sandboxes.UnpausePaused(ctx); err != nil {
		logger.Warn("paused-container reconciliation incomplete", slog.String("error", err.Error()))
	} else if n > 0 {
		logger.Info("unpaused containers left frozen by a previous run", slog.Int("count", n))
	}
	if err := execs.Reconcile(ctx); err != nil {
		logger.Warn("exec reconciliation incomplete", slog.String("error", err.Error()))
	}
	// The two services depend on each other: snapshot.Service needs the sandbox lock via
	// WithSnapshotSource, sandbox.Service needs Resolve to fork and restore. Neither needs the
	// other at construction, so the cycle is broken by wiring the second half after the fact.
	// Leaving SetSnapshots uncalled compiles fine and nil-panics on the first fork at runtime.
	snapshots := snapshot.NewService(store.Snapshots(), sandboxes, rt)
	sandboxes.SetSnapshots(snapshots)

	server := &http.Server{
		Addr: cfg.Addr,
		Handler: api.NewServer(api.Options{
			Sandboxes: sandboxes,
			Execs:     execs,
			Snapshots: snapshots,
			TokenHash: tokenHash,
			Version:   version,
			Logger:    logger,
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errs := make(chan error, 1)
	go func() {
		logger.Info("listening", slog.String("addr", cfg.Addr))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Warn("graceful shutdown incomplete", slog.String("error", err.Error()))
	}
	if err := execs.Shutdown(shutdownCtx); err != nil {
		logger.Warn("exec shutdown incomplete", slog.String("error", err.Error()))
	}
	return nil
}
