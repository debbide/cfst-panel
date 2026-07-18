package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/debbide/cfst-panel/internal/api"
	"github.com/debbide/cfst-panel/internal/cfstbin"
	"github.com/debbide/cfst-panel/internal/config"
	"github.com/debbide/cfst-panel/internal/scheduler"
	"github.com/debbide/cfst-panel/internal/service"
	"github.com/debbide/cfst-panel/internal/store"
)

func main() {
	addr := flag.String("addr", "", "listen address, overrides settings")
	dataDir := flag.String("data", "", "data directory (default: executable directory)")
	webDir := flag.String("web", "", "optional external frontend directory; empty means use embedded UI")
	flag.Parse()

	exeDir, err := executableDir()
	if err != nil {
		log.Fatalf("resolve executable dir: %v", err)
	}

	absData := *dataDir
	if absData == "" {
		absData = exeDir
	}
	absData, err = filepath.Abs(absData)
	if err != nil {
		log.Fatalf("resolve data dir: %v", err)
	}
	if err := os.MkdirAll(absData, 0o755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}

	// Extract bundled CloudflareST core next to the binary/data dir.
	bundledBinary, err := cfstbin.Ensure(absData)
	if err != nil {
		log.Printf("warn: extract bundled CloudflareST failed: %v", err)
	} else {
		log.Printf("cloudflareST ready: %s", bundledBinary)
	}

	st, err := store.Open(filepath.Join(absData, "panel.json"))
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	cfgSvc := config.NewService(st, absData)
	settings, err := cfgSvc.EnsureDefaults()
	if err != nil {
		log.Fatalf("load settings: %v", err)
	}
	// If user has not customized binary path, prefer bundled one.
	if bundledBinary != "" {
		if settings.CFSTBinary == "" || settings.CFSTBinary == filepath.Join(absData, "CloudflareST") || settings.CFSTBinary == "CloudflareST" || settings.CFSTBinary == filepath.Join("bin", "CloudflareST") {
			settings.CFSTBinary = bundledBinary
		}
		if settings.CFSTIPFile == "" {
			ipFile := filepath.Join(absData, "ip.txt")
			if _, err := os.Stat(ipFile); err == nil {
				settings.CFSTIPFile = ipFile
			}
		}
		if err := cfgSvc.Save(settings); err != nil {
			log.Printf("warn: save bundled path settings: %v", err)
		}
	}

	app := service.New(st, cfgSvc, absData)
	sched := scheduler.New(app)
	if err := sched.Reload(settings); err != nil {
		log.Printf("scheduler init: %v", err)
	}
	app.SetScheduler(sched)

	listen := settings.ListenAddr
	if *addr != "" {
		listen = *addr
	}
	app.SetListenAddr(listen)

	server := api.NewServer(app, *webDir, listen)
	httpServer := &http.Server{
		Addr:              listen,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("CFST Panel listening on http://%s", listen)
		log.Printf("data dir: %s", absData)
		if *webDir != "" {
			log.Printf("web dir: %s (external)", *webDir)
		} else {
			log.Printf("web ui: embedded")
		}
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sched.Stop()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(os.Stderr, "shutdown: %v\n", err)
	}
}

func executableDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		// Fall back to the raw executable path when symlink resolution fails.
		exe, err = os.Executable()
		if err != nil {
			return "", err
		}
	}
	return filepath.Dir(exe), nil
}
