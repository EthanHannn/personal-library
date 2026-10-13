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
)

func main() {
	if err := run(); err != nil {
		slog.Error("library stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: library probe [flags] | healthcheck [--url URL] | metrics")
	}
	if os.Args[1] == "metrics" {
		metrics, err := probe.ContainerMetrics()
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(metrics)
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
	if os.Args[1] != "probe" {
		return fmt.Errorf("unknown command: %s", os.Args[1])
	}
	fs := flag.NewFlagSet("probe", flag.ContinueOnError)
	db := fs.String("db", "", "read-only legacy SQLite snapshot")
	novels := fs.String("novels", "", "read-only novel root")
	manifestPath := fs.String("manifest", "", "verified encoding / file-version manifest")
	listen := fs.String("listen", "127.0.0.1:8080", "HTTP listen address; only a trusted gateway may access it")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return err
	}
	if *db == "" || *novels == "" || *manifestPath == "" || fs.NArg() > 0 {
		return fmt.Errorf("probe requires --db, --novels and --manifest")
	}
	config, err := gateway.Parse(os.Getenv("PORTAL_ALLOWED_USER"), os.Getenv("LIBRARY_PUBLIC_URL"), os.Getenv("PORTAL_AUTH_URL"))
	if err != nil {
		return err
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
	app, err := probe.New(store, r, manifest)
	if err != nil {
		return err
	}
	plan, err := store.QueryPlan(ctx)
	if err != nil {
		return err
	}
	slog.Info("read-only probe ready", "listen", *listen, "queryPlan", plan)
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
