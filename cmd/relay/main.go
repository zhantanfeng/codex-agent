package main

import (
	"flag"
	"log/slog"
	"os"

	"codexremote/internal/relay"
)

func main() {
	address := flag.String("listen", ":8080", "HTTP listen address")
	flag.Parse()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := relay.ListenAndServe(*address, log); err != nil {
		log.Error("relay exited", "error", err)
		os.Exit(1)
	}
}
