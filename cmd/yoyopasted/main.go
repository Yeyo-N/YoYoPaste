package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/Yeyo-N/YoYoPaste/internal/peer"
	"github.com/Yeyo-N/YoYoPaste/internal/store"
	"github.com/Yeyo-N/YoYoPaste/internal/sync"
	"github.com/Yeyo-N/YoYoPaste/internal/tray"
	"github.com/Yeyo-N/YoYoPaste/internal/ui"
)

var verbose = flag.Bool("v", false, "verbose logging")

func main() {
	flag.Parse()

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	slog.Info("yoyopasted starting", "version", "dev")

	// Determine store dir: OS user config dir
	dir := storeDir()
	slog.Info("store dir", "dir", dir)
	st, err := store.Open(dir)
	if err != nil {
		slog.Error("open store", "err", err)
		os.Exit(1)
	}
	defer store.Close(st)

	peerSrv := peer.New(st, "dev")
	eng := sync.New(st, peerSrv)
	uiSrv := ui.NewWithStore(eng, st)

	// Run servers
	go func() {
		if err := peerSrv.ListenAndServe(ctx); err != nil {
			slog.Error("peer server", "err", err)
		}
	}()
	go func() {
		if err := uiSrv.ListenAndServe(ctx); err != nil {
			slog.Error("ui server", "err", err)
		}
	}()
	go func() {
		if err := eng.Run(ctx); err != nil {
			slog.Error("sync engine", "err", err)
		}
	}()

	// Tray runs on main thread on some platforms; if it returns, cancel.
	// For headless/CI, skip tray if no display.
	if hasDisplay() {
		tray.Run(ctx, eng)
		cancel()
	} else {
		<-ctx.Done()
	}

	slog.Info("yoyopasted shutting down")
}

func storeDir() string {
	if d := os.Getenv("YOYOPASTE_DIR"); d != "" {
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "./yoyopaste-data"
	}
	return filepath.Join(base, "YoYoPaste")
}

func hasDisplay() bool {
	// Skip tray in CI or headless
	if os.Getenv("CI") != "" {
		return false
	}
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		// On macOS, assume display exists if not CI
		if os.Getenv("CI") == "" {
			// Check if we are on darwin with window server? Just return true for non-linux
			return true
		}
		return false
	}
	return true
}
