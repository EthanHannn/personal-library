package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/EthanHannn/personal-library/internal/gateway"
	"github.com/EthanHannn/personal-library/internal/reader"
	"github.com/EthanHannn/personal-library/internal/service"
	"github.com/EthanHannn/personal-library/internal/storage"
	"github.com/EthanHannn/personal-library/web"
)

func serve(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	dbPath := flags.String("db", "", "migrated business database")
	novels := flags.String("novels", "", "immutable novel root")
	listen := flags.String("listen", "127.0.0.1:8080", "HTTP listen address; only a trusted gateway may access it")
	webDir := flags.String("web-dir", "", "optional development override for the built static app")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *dbPath == "" || *novels == "" || flags.NArg() != 0 {
		return fmt.Errorf("serve requires --db and --novels")
	}
	config, err := gateway.Parse(os.Getenv("PORTAL_ALLOWED_USER"), os.Getenv("LIBRARY_PUBLIC_URL"), os.Getenv("PORTAL_AUTH_URL"))
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := storage.Open(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	r, err := reader.Open(*novels, 4)
	if err != nil {
		return err
	}
	defer r.Close()
	var assets fs.FS
	if *webDir != "" {
		assets = os.DirFS(*webDir)
	} else {
		assets, err = web.Assets()
		if err != nil {
			return err
		}
	}
	handler, err := service.New(ctx, db, r, config, assets)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 * 1024}
	slog.Info("library ready", "listen", *listen, "schemaVersion", storage.Version)
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
