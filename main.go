package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
)

var version = "dev"

func main() {
	port := flag.Int("port", 6062, "listen port")
	host := flag.String("host", "127.0.0.1", "bind address")
	token := flag.String("token", "", "auth token (auto-generated if empty)")
	dev := flag.Bool("dev", false, "use filesystem instead of embedded assets")
	flag.Parse()

	if args := flag.Args(); len(args) > 0 {
		switch args[0] {
		case "version":
			fmt.Printf("tmuxui %s\n", version)
			return
		case "update":
			if err := runUpdate(); err != nil {
				log.Fatal(err)
			}
			return
		}
	}

	if *token == "" {
		*token = os.Getenv("TMUXUI_TOKEN")
	}
	if *token == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			log.Fatal(err)
		}
		*token = hex.EncodeToString(b)
	}

	hub := newHub()
	go hub.run()

	addr := fmt.Sprintf("%s:%d", *host, *port)
	srv := newServer(*token, hub, *dev)

	fmt.Printf("tmuxui %s\n", version)
	fmt.Printf("Listening on http://%s\n", addr)
	fmt.Printf("Access URL: http://%s?token=%s\n", addr, *token)
	log.Fatal(http.ListenAndServe(addr, srv))
}
