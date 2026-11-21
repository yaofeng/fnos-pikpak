package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"fnos-pikpak/internal/fnos"
	"fnos-pikpak/internal/httpapi"
	"fnos-pikpak/internal/probe"
	"fnos-pikpak/internal/version"
)

func defaultSocket() string {
	if dest := os.Getenv("TRIM_APPDEST"); dest != "" {
		return filepath.Join(dest, "app.sock")
	}
	return "./app.sock"
}

func main() {
	socket := flag.String("socket", defaultSocket(), "unix socket path to listen on")
	listen := flag.String("listen", "", "development only: listen on a TCP address instead of the unix socket")
	devUID := flag.Int("dev-uid", 0, "development only: identity to use when the gateway header is absent")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version.Version)
		return
	}

	fnosClient := &fnos.Client{
		SocketPath: "/var/run/trim_open_gateway_apiscope.socket",
		AppName:    "pikpak",
	}
	probes := probe.NewRegistry()
	fnos.RegisterProbes(probes, fnosClient)

	handler := httpapi.NewMux(httpapi.Deps{
		Version: version.Version,
		DevUID:  *devUID,
		Probes:  probes,
		Fnos:    fnosClient,
	})
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	if *listen != "" {
		ln, err := net.Listen("tcp", *listen)
		if err != nil {
			fmt.Fprintf(os.Stderr, "listen %s: %v\n", *listen, err)
			os.Exit(1)
		}
		srv := &http.Server{Handler: handler}
		go func() { _ = srv.Serve(ln) }()
		fmt.Printf("listening on %s\n", ln.Addr())
		<-signals
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		return
	}

	stop, err := httpapi.Start(*socket, handler)
	if err != nil {
		fmt.Fprintf(os.Stderr, "listen %s: %v\n", *socket, err)
		os.Exit(1)
	}
	fmt.Printf("listening on %s\n", *socket)
	<-signals
	_ = stop()
}
