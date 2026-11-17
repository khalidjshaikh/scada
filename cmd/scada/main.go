package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kshaikh/scada/internal/api"
	"github.com/kshaikh/scada/internal/config"
	"github.com/kshaikh/scada/internal/core/alarm"
	"github.com/kshaikh/scada/internal/core/db"
	"github.com/kshaikh/scada/internal/core/event"
	"github.com/kshaikh/scada/internal/core/history"
	"github.com/kshaikh/scada/internal/core/tag"
	"github.com/kshaikh/scada/internal/driver"
	"github.com/kshaikh/scada/internal/web"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: scada [run|version]")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version":
		fmt.Println("scada 0.1.0")
	case "run":
		runServer(os.Args[2:])
	default:
		fmt.Println("unknown command")
		os.Exit(2)
	}
}

func runServer(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	cfgPath := fs.String("config", "config.json", "config file path")
	fs.StringVar(cfgPath, "c", "config.json", "config file path (short)")
	fs.Parse(args)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Println("config error:", err)
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		fmt.Println("config invalid:", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	bus := event.New(256)
	database := db.New(bus)

	for _, tc := range cfg.Tags {
		t := tag.New(tc.ID, tc.Name, tag.Type(tc.Type))
		t.Description = tc.Description
		t.Unit = tc.Unit
		t.Group = tc.Group
		if tc.Access != "" {
			t.Access = tc.Access
		}
		t.Min = tc.Min
		t.Max = tc.Max
		t.Metadata = tc.Metadata
		if err := database.Add(t); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	}

	hist, err := history.New(cfg.History.Path)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer hist.Close()

	ch := bus.Subscribe("tag:*")
	go func() {
		for ev := range ch {
			if snap, ok := ev.Data.(tag.Snapshot); ok {
				hist.Record(snap.ID, snap)
			}
		}
	}()

	alm := alarm.NewManager(database, bus)
	for _, ac := range cfg.Alarms {
		alm.AddRule(alarm.Rule{
			ID:        ac.ID,
			Name:      ac.Name,
			TagID:     ac.TagID,
			Type:      ac.Type,
			Threshold: ac.Threshold,
			Priority:  alarm.Priority(ac.Priority),
			Enabled:   ac.Enabled,
			Deadband:  ac.Deadband,
			Message:   ac.Message,
		})
	}

	dm := driver.NewManager(database)
	sim := driver.NewSimulated("sim", time.Second)
	dm.Add(sim)
	for _, mc := range cfg.Modbus {
		interval := 2 * time.Second
		if mc.Interval != "" {
			if d, err := time.ParseDuration(mc.Interval); err == nil {
				interval = d
			}
		}
		md := driver.NewModbus(mc.Name, mc.Host, mc.Port, byte(mc.Unit), interval, mc.Maps)
		dm.Add(md)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dm.Start(ctx)

	apiServer := &api.API{
		DB:      database,
		Alarms:  alm,
		History: hist,
		Bus:     bus,
		DriverWrite: func(tagID string, value any) error {
			return dm.Write(ctx, tagID, value)
		},
		WebFS: web.FS(),
	}
	handler := apiServer.Handler()

	srv := &http.Server{Addr: cfg.Server.HTTPAddr, Handler: handler}
	go func() {
		logger.Info("SCADA server listening", "addr", cfg.Server.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "err", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	logger.Info("shutting down")
	cancel()
	dm.Stop()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	srv.Shutdown(shutdownCtx)
}
