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
	"fnos-pikpak/internal/pikpak"
	"fnos-pikpak/internal/probe"
	"fnos-pikpak/internal/store"
	"fnos-pikpak/internal/version"
)

func defaultSocket() string {
	if dest := os.Getenv("TRIM_APPDEST"); dest != "" {
		return filepath.Join(dest, "app.sock")
	}
	return "./app.sock"
}

func dataDir() string {
	if v := os.Getenv("TRIM_PKGVAR"); v != "" {
		return v
	}
	return "./.data"
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

	dir := dataDir()
	st, err := store.Open(filepath.Join(dir, "pikpak.db"), filepath.Join(dir, "secret.key"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "open store in %s: %v\n", dir, err)
		os.Exit(1)
	}
	defer st.Close()

	// The fnOS Open API gateway only exists on the NAS. In TCP development mode
// there is nothing to talk to, so the fnOS probes skip and the
// file-authorization card reports the service as unavailable.
var fnosClient *fnos.Client
if *listen == "" {
	fnosClient = &fnos.Client{
		SocketPath: "/var/run/trim_open_gateway_apiscope.socket",
		AppName:    "pikpak",
	}
}
probes := probe.NewRegistry()
fnos.RegisterProbes(probes, fnosClient)

	sessions := pikpak.NewSessionStore(st, pikpak.NewClient(pikpak.ClientOptions{}))
	httpapi.RegisterPikPakProbes(probes, sessions)

	handler := httpapi.NewMux(httpapi.Deps{
		Version:  version.Version,
		DevUID:   *devUID,
		Probes:   probes,
		Fnos:     fnosClient,
		Store:    st,
		Sessions: sessions,
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
