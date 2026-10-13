package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/EthanHannn/personal-library/internal/gateway"
	"github.com/EthanHannn/personal-library/internal/legacy"
	"github.com/EthanHannn/personal-library/internal/probe"
	"github.com/EthanHannn/personal-library/internal/reader"
	"github.com/EthanHannn/personal-library/internal/storage"
)

func main() {
	if err := run(); err != nil {
		slog.Error("library stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: library serve | migrate | import-legacy | probe | verify-legacy | healthcheck | metrics")
	}
	if os.Args[1] == "serve" {
		return serve(os.Args[2:])
	}
	if os.Args[1] == "migrate" {
		fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
		db := fs.String("db", "", "new business database (never the legacy snapshot)")
		if err := fs.Parse(os.Args[2:]); err != nil {
			return err
		}
		if *db == "" || fs.NArg() != 0 {
			return fmt.Errorf("migrate requires --db")
		}
		return storage.Migrate(context.Background(), *db)
	}
	if os.Args[1] == "import-legacy" {
		return importLegacy(os.Args[2:])
	}
	if os.Args[1] == "metrics" {
		metrics, err := probe.ContainerMetrics()
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(metrics)
	}
	if os.Args[1] == "fixture-cover" {
		fs := flag.NewFlagSet("fixture-cover", flag.ContinueOnError)
		output := fs.String("output", "", "new synthetic JPEG file")
		if err := fs.Parse(os.Args[2:]); err != nil {
			return err
		}
		if *output == "" || fs.NArg() != 0 {
			return fmt.Errorf("fixture-cover requires --output")
		}
		return probe.CreateCoverFixture(*output)
	}
	if os.Args[1] == "healthcheck" {
		fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
		url := fs.String("url", "http://127.0.0.1:8080/api/health", "internal health endpoint")
		if err := fs.Parse(os.Args[2:]); err != nil {
			return err
		}
		client := http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get(*url)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("health status %d", resp.StatusCode)
		}
		return nil
	}
	if os.Args[1] != "probe" && os.Args[1] != "verify-legacy" {
		return fmt.Errorf("unknown command: %s", os.Args[1])
	}
	fs := flag.NewFlagSet("probe", flag.ContinueOnError)
	db := fs.String("db", "", "read-only legacy SQLite snapshot")
	novels := fs.String("novels", "", "read-only novel root")
	manifestPath := fs.String("manifest", "", "verified encoding / file-version manifest")
	listen := fs.String("listen", "127.0.0.1:8080", "HTTP listen address; only a trusted gateway may access it")
	imageWorkload := fs.String("image-workload", "", "M0 only: synthetic image decoded every 10s to test background memory")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return err
	}
	if *db == "" || *novels == "" || *manifestPath == "" || fs.NArg() > 0 {
		return fmt.Errorf("probe requires --db, --novels and --manifest")
	}
	manifest, err := probe.LoadManifest(*manifestPath)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	store, err := legacy.Open(ctx, *db)
	if err != nil {
		return err
	}
	defer store.Close()
	r, err := reader.Open(*novels, 4)
	if err != nil {
		return err
	}
	defer r.Close()
	if os.Args[1] == "verify-legacy" {
		report, err := probe.Verify(ctx, store, r, manifest)
		if encodeErr := json.NewEncoder(os.Stdout).Encode(report); encodeErr != nil {
			return encodeErr
		}
		if err != nil {
			return err
		}
		return report.Error()
	}
	config, err := gateway.Parse(os.Getenv("PORTAL_ALLOWED_USER"), os.Getenv("LIBRARY_PUBLIC_URL"), os.Getenv("PORTAL_AUTH_URL"))
	if err != nil {
		return err
	}
	app, err := probe.New(store, r, manifest)
	if err != nil {
		return err
	}
	plan, err := store.QueryPlan(ctx)
	if err != nil {
		return err
	}
	slog.Info("read-only probe ready", "listen", *listen, "queryPlan", plan)
	if *imageWorkload != "" {
		if err := probe.DecodeCoverSample(ctx, *imageWorkload); err != nil {
			return err
		}
		go probe.ImageWorkload(ctx, *imageWorkload, 10*time.Second)
	}
	server := &http.Server{Addr: *listen, Handler: app.Handler(config), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 * 1024}
	result := make(chan error, 1)
	go func() { result <- server.ListenAndServe() }()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}
