// damocline-labtarget serves a bounded synthetic application for local tests.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/labtarget"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "synthetic target listen address")
	delay := flag.Duration("delay", 100*time.Millisecond, "bounded work time (1ms..1s)")
	parallel := flag.Int("parallel", 1, "simultaneous work slots (1..16)")
	queue := flag.Int("queue", 16, "total admitted requests (parallel..64)")
	flag.Parse()
	if len(flag.Args()) != 0 {
		fmt.Fprintln(os.Stderr, "positional arguments are not accepted")
		os.Exit(2)
	}
	handler, err := labtarget.NewHandler(labtarget.Options{Delay: *delay, Parallel: *parallel, QueueLimit: *queue})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	server := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
