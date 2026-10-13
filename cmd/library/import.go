package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/EthanHannn/personal-library/internal/gateway"
	"github.com/EthanHannn/personal-library/internal/importer"
	"github.com/EthanHannn/personal-library/internal/probe"
)

func importLegacy(args []string) error {
	fs := flag.NewFlagSet("import-legacy", flag.ContinueOnError)
	var o importer.Options
	fs.StringVar(&o.Source, "source", "", "offline SQLite-consistent legacy snapshot")
	fs.StringVar(&o.Target, "db", "", "separate migrated target database")
	fs.StringVar(&o.Novels, "novels", "", "immutable novel root")
	fs.StringVar(&o.PortalUser, "portal-user", "", "trusted gateway user mapped to legacy default-reader")
	fs.StringVar(&o.SourceVersion, "source-version", "", "legacy release / synthetic fixture identifier")
	fs.IntVar(&o.BatchSize, "batch-size", 5000, "rows per transaction (1..10000)")
	fs.IntVar(&o.MaxBatches, "max-batches", 0, "stop at a durable checkpoint after N batches; zero continues to completion")
	fs.DurationVar(&o.BatchPause, "batch-pause", 0, "optional I/O throttle between batches (maximum 1m)")
	manifest := fs.String("manifest", "", "verified encoding and file-version manifest")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if o.Source == "" || o.Target == "" || o.Novels == "" || *manifest == "" || o.SourceVersion == "" || fs.NArg() != 0 {
		return fmt.Errorf("import-legacy requires --source --db --novels --manifest --portal-user --source-version")
	}
	// Reuse the portal username contract without requiring live URL config for
	// an offline operation.
	if _, err := gateway.Parse(o.PortalUser, "https://library.example.test", "https://auth.example.test"); err != nil {
		return err
	}
	m, err := probe.LoadManifest(*manifest)
	if err != nil {
		return err
	}
	o.Files = m.Files
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	report, err := importer.Import(ctx, o)
	if encodeErr := json.NewEncoder(os.Stdout).Encode(report); encodeErr != nil {
		return encodeErr
	}
	return err
}
