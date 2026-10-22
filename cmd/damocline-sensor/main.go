// damocline-sensor serves bounded Linux host observations for authorized tests.
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

	"github.com/LucasLuchetta/Damocline/internal/hostsensor"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:9467", "sensor listen address")
	tokenEnv := flag.String("token-env", "DAMOCLINE_SENSOR_TOKEN", "environment variable holding the sensor bearer token")
	certFile := flag.String("tls-cert", "", "TLS certificate file for a remote listener")
	keyFile := flag.String("tls-key", "", "TLS private key file for a remote listener")
	workloadPID := flag.Int("workload-pid", 0, "optional PID to inspect process RSS/CPU and visible cgroup-v2 limits and memory events")
	flag.Parse()
	if len(flag.Args()) != 0 {
		fmt.Fprintln(os.Stderr, "positional arguments are not accepted")
		os.Exit(2)
	}
	if err := hostsensor.ValidateBind(*listen, *certFile, *keyFile); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	token := os.Getenv(*tokenEnv)
	if *workloadPID < 0 {
		fmt.Fprintln(os.Stderr, "workload PID must not be negative")
		os.Exit(2)
	}
	handler, err := hostsensor.NewHandler(token, hostsensor.NativeCollector(*workloadPID))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	server := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 10 * time.Second, MaxHeaderBytes: 4096}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	if *certFile != "" {
		err = server.ListenAndServeTLS(*certFile, *keyFile)
	} else {
		err = server.ListenAndServe()
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
