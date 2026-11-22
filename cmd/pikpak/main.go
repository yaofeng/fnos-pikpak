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
	"strconv"
	"syscall"
	"time"

	"fnos-pikpak/internal/fnos"
	"fnos-pikpak/internal/httpapi"
	"fnos-pikpak/internal/pikpak"
	"fnos-pikpak/internal/playtoken"
	"fnos-pikpak/internal/probe"
	"fnos-pikpak/internal/store"
	"fnos-pikpak/internal/transfer"
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

	// A job left running at shutdown has no worker; its .part file makes the
	// download resumable, so it goes back to the queue.
	startupCtx := context.Background()
	_ = st.ResetRunningDownloadJobs(startupCtx)
	_ = st.ResetRunningUploadJobs(startupCtx)

	queue := &transfer.Queue{
		Store: st,
		Clients: func(ctx context.Context, uid int) (*pikpak.Client, error) {
			return sessions.ClientFor(ctx, uid)
		},
		MaxGlobal: 2,
	}
	if v, err := st.GetSetting(startupCtx, "download_concurrency"); err == nil {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 4 {
			queue.MaxGlobal = n
		}
	}
	queueCtx, queueCancel := context.WithCancel(context.Background())
	queue.Start(queueCtx)
	defer func() {
		queueCancel()
		done := make(chan struct{})
		go func() { queue.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
	}()

	deps := httpapi.Deps{
		Version: version.Version,
		// Stream tokens get their own key derived from the master key, so a
		// leaked URL cannot be turned back into the credential key.
		PlaySecret: playtoken.DeriveSecret(st.SecretKey()),
		DevUID:     *devUID,
		Probes:     probes,
		Fnos:       fnosClient,
		Store:      st,
		Sessions:   sessions,
		Queue:      queue,
	}
	// Local development has no fnOS gateway, so the ACL question cannot be
	// answered; allow absolute paths so the transfer flows stay testable.
	if *devUID != 0 && fnosClient == nil {
		deps.DirGuard = httpapi.DevDirGuard()
	}
	handler := httpapi.NewMux(deps)
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
