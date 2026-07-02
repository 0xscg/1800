package main

import (
	"context"
	"log"
	"net/http"

	"github.com/sushan/longevity/internal/config"
	"github.com/sushan/longevity/internal/httpapi"
	"github.com/sushan/longevity/internal/store"
	"github.com/sushan/longevity/internal/whoop"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer st.Pool.Close()
	if err := st.Pool.Ping(ctx); err != nil {
		log.Fatalf("db ping: %v", err)
	}

	api := &httpapi.API{
		Cfg:   cfg,
		Store: st,
		Whoop: whoop.New(cfg.WhoopClientID, cfg.WhoopClientSecret, cfg.WhoopRedirectURL, st),
	}

	log.Printf("listening on %s", cfg.HTTPAddr)
	log.Fatal(http.ListenAndServe(cfg.HTTPAddr, api.Router()))
}
