package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tunapanel/internal/config"
	"tunapanel/internal/web"
)

func main() {
	if os.Geteuid() == 0 {
		fmt.Fprintln(os.Stderr, "tunapanel must not run as root")
		os.Exit(1)
	}

	client := web.DefaultAgentClient(config.SocketPath)
	server, err := web.NewServer(client)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to initialize web UI:", err)
		os.Exit(1)
	}

	httpServer := &http.Server{
		Addr:              server.Addr,
		Handler:           server.Handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	log.Printf("tunapanel web UI listening on http://%s", server.Addr)

	errCh := make(chan error, 1)
	go func() {
		errCh <- httpServer.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("signal received: %s", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			log.Printf("shutdown error: %v", err)
		}
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("server error: %v", err)
		}
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}
}
