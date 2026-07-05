package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/lieyan/firescribe/internal/api"
	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/config"
	"github.com/lieyan/firescribe/internal/db"
	"github.com/lieyan/firescribe/internal/recognizer"
	"github.com/lieyan/firescribe/internal/storage"
	"github.com/lieyan/firescribe/internal/updater"
	"github.com/lieyan/firescribe/internal/version"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	conn, err := db.Open(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}

	if err := db.Migrate(conn); err != nil {
		log.Fatalf("migrate database: %v", err)
	}

	files, err := storage.New(cfg.DataDir)
	if err != nil {
		log.Fatalf("prepare storage: %v", err)
	}

	application := app.New(app.NewStore(conn), files, recognizer.Build(cfg))
	application.SetOptions(app.Options{PDFRenderDPI: cfg.PDFRenderDPI})

	runtime := config.NewRuntime(cfg)
	runtime.OnApply(func(next config.Config) {
		application.SetRecognizer(recognizer.Build(next))
		application.SetOptions(app.Options{PDFRenderDPI: next.PDFRenderDPI})
	})

	if n, err := application.Store.RecoverInterrupted(context.Background()); err != nil {
		log.Printf("recover interrupted jobs: %v", err)
	} else if n > 0 {
		log.Printf("marked %d interrupted job(s) as failed", n)
	}

	bgCtx, cancelBackground := context.WithCancel(context.Background())
	defer cancelBackground()

	var closeOnce sync.Once
	closeResources := func() {
		closeOnce.Do(func() {
			_ = conn.Close()
		})
	}
	defer closeResources()

	// stopWorkers cancels active recognition runs and waits for their workers
	// to persist terminal state before the process exits or re-execs.
	stopWorkers := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		application.Shutdown(ctx)
	}

	var restartMu sync.Mutex
	restarting := false
	restartErrCh := make(chan error, 1)
	markRestarting := func() {
		restartMu.Lock()
		restarting = true
		restartMu.Unlock()
	}
	isRestarting := func() bool {
		restartMu.Lock()
		defer restartMu.Unlock()
		return restarting
	}

	var server *http.Server
	upd := updater.New(
		func() updater.Config { return cfg.Update },
		func() string { return cfg.DataDir },
		log.Default(),
		updater.RestartHooks{
			BeforeExec: func(tag string) error {
				markRestarting()
				cancelBackground()
				stopWorkers()

				shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
					return err
				}
				closeResources()
				log.Printf("update: prepared restart for %s", tag)
				return nil
			},
			OnExecFailure: func(err error) {
				select {
				case restartErrCh <- err:
				default:
				}
			},
			IsBusy: func() bool {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				busy, err := application.Store.HasActiveJobs(ctx)
				if err != nil {
					return false
				}
				return busy
			},
		},
	)

	server = &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.New(application, cfg.WebDir, runtime, api.UpdateRuntime{Updater: upd, Config: cfg.Update}).Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	upd.StartBackground(bgCtx)

	errCh := make(chan error, 1)
	go func() {
		log.Printf("FireScribe %s (commit=%s, built=%s)", version.Version, version.Commit, version.BuildTime)
		log.Printf("FireScribe listening on http://localhost%s", cfg.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- http.ErrServerClosed
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			if isRestarting() {
				if err := <-restartErrCh; err != nil {
					log.Fatal(err)
				}
			}
			return
		}
		log.Fatalf("serve: %v", err)
	case <-stop:
		cancelBackground()
		stopWorkers()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	case err := <-restartErrCh:
		log.Fatal(err)
	}
}
