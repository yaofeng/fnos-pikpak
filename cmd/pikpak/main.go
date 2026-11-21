package main

import (
	"flag"
	"fmt"
	"os"

	"fnos-pikpak/internal/version"
)

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version.Version)
		return
	}

	fmt.Fprintf(os.Stderr, "pikpak %s: service entrypoint not wired yet\n", version.Version)
	os.Exit(1)
}
