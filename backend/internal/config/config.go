package config

import "os"

type Config struct {
	DatabaseURL       string
	HTTPAddr          string
	WhoopClientID     string
	WhoopClientSecret string
	WhoopRedirectURL  string
	DeviceIngestToken string
	WebOrigin         string
	WebDist           string // if set, serve the built web app from this dir
}

func Load() Config {
	get := func(k, d string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return d
	}
	return Config{
		DatabaseURL:       get("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/longevity?sslmode=disable"),
		HTTPAddr:          get("HTTP_ADDR", ":8080"),
		WhoopClientID:     os.Getenv("WHOOP_CLIENT_ID"),
		WhoopClientSecret: os.Getenv("WHOOP_CLIENT_SECRET"),
		WhoopRedirectURL:  get("WHOOP_REDIRECT_URL", "http://localhost:8080/v1/connect/whoop/callback"),
		DeviceIngestToken: get("DEVICE_INGEST_TOKEN", "change-me"),
		WebOrigin:         get("WEB_ORIGIN", "http://localhost:5173"),
		WebDist:           os.Getenv("WEB_DIST"),
	}
}
