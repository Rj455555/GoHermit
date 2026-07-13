package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	webui "github.com/Rj455555/GoHermit/internal/web"
)

func main() {
	listen := flag.String("listen", env("GOHERMIT_LISTEN", "127.0.0.1:8787"), "listen address")
	workspace := flag.String("workspace", env("GOHERMIT_WORKSPACE", "."), "fixed agent workspace")
	configPath := flag.String("config", env("GOHERMIT_CONFIG", ""), "configuration file")
	flag.Parse()
	server, err := webui.New(*workspace, *configPath)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Printf("GoHermit Web %s listening on http://%s\n", "0.2.0-dev", *listen)
	if err := webui.ListenAndServe(ctx, *listen, server); err != nil {
		log.Fatal(err)
	}
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
