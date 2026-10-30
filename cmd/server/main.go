package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	"webrom/ytdlbot/internal/app"
	"webrom/ytdlbot/internal/safeurl"
)

func main() {
	if e := run(); e != nil {
		slog.Error(e.Error())
		os.Exit(1)
	}
}
func run() error {
	if e := app.LoadEnv(".env"); e != nil {
		return e
	}
	c, e := app.LoadConfig()
	if e != nil {
		return e
	}
	os.MkdirAll(c.DataDir, 0700)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	setup, done := context.WithTimeout(ctx, 20*time.Second)
	defer done()
	store, e := app.OpenStore(setup, c)
	if e != nil {
		return e
	}
	defer store.Pool.Close()
	lock, e := store.Pool.Acquire(setup)
	if e != nil {
		return e
	}
	defer lock.Release()
	var owned bool
	if e = lock.QueryRow(setup, "SELECT pg_try_advisory_lock(819534762)").Scan(&owned); e != nil {
		return e
	}
	if !owned {
		slog.Error("ya hay otra instancia usando esta base de datos")
		return nil
	}
	defer lock.Exec(context.Background(), "SELECT pg_advisory_unlock(819534762)")
	proxyListener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return e
	}
	proxy := &http.Server{Handler: safeurl.NewProxy(), ReadHeaderTimeout: 10 * time.Second}
	go proxy.Serve(proxyListener)
	defer proxy.Close()
	engine := &app.Engine{Config: c, ProxyURL: "http://" + proxyListener.Addr().String(), HTTP: safeurl.Client()}
	server := app.NewServer(c, store, engine)
	web := &http.Server{Addr: c.Addr, Handler: server.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 32768}
	var wg sync.WaitGroup
	server.Workers(ctx, &wg)
	wg.Add(1)
	go func() { defer wg.Done(); server.RunBot(ctx) }()
	failures := make(chan error, 1)
	go func() {
		slog.Info("Mini App disponible", "url", "http://"+c.Addr, "workers", c.Workers, "telegram_configurado", c.Token != "")
		failures <- web.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
	case e = <-failures:
		if e != http.ErrServerClosed {
			cancel()
		}
	}
	cancel()
	stop, finish := context.WithTimeout(context.Background(), 20*time.Second)
	defer finish()
	web.Shutdown(stop)
	wg.Wait()
	return e
}
