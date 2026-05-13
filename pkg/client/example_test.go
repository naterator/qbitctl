package client_test

import (
	"bytes"
	"context"
	"io"
	"log"
	"time"

	"github.com/naterator/qbitctl/pkg/client"
)

func ExampleNewClient_libraryUsage() {
	app, err := client.NewClient(&client.Options{
		URL:  "http://localhost:8080",
		User: "admin",
		Pass: "secret",
	})
	if err != nil {
		log.Fatal(err)
	}

	app.Stdout = &bytes.Buffer{}
	app.Stderr = io.Discard

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	app.Ctx = ctx

	hash, err := app.ResolveHash("9c328901")
	if err != nil {
		log.Fatal(err)
	}
	if err := app.ShowSingleTorrentInfo(hash); err != nil {
		log.Fatal(err)
	}
	if err := app.SetField(hash, "category", "linux"); err != nil {
		log.Fatal(err)
	}
	if err := app.StartTorrent(hash); err != nil {
		log.Fatal(err)
	}
}
