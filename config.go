package main

import (
    "errors"
    "fmt"
    "github.com/joho/godotenv"

    "os"
    "strconv"
    "strings"
)


// ClientConfig holds environment configuration for the exporter.
// It is intentionally immutable after construction.
type ClientConfig struct {
    OLHURL       string
    OLHHost      string
    InfluxURL    string
    InfluxOrg    string
    InfluxDB     string
    InfluxToken  string
    PollInterval int // seconds, default 30
}

// LoadConfig reads environment variables, validates required ones, and returns a configuration.
func LoadConfig() (*ClientConfig, error) {
    // Load .env if available
    if err := godotenvLoad(); err != nil {
        // log warning but continue; environment may be set elsewhere
    }

    cfg := &ClientConfig{
        OLHURL:       os.Getenv("OLHURL"),
        OLHHost:      os.Getenv("OLHHOST"),
        InfluxURL:    os.Getenv("INFLUX_URL"),
        InfluxOrg:    os.Getenv("INFLUX_ORG"),
        InfluxDB:     os.Getenv("INFLUX_DB"),
        InfluxToken:  os.Getenv("INFLUX_TOKEN"),
        PollInterval: 30,
    }

    // Fall back to underscore variant
    if cfg.OLHURL == "" {
        cfg.OLHURL = os.Getenv("OLH_URL")
    }
    if cfg.InfluxURL == "" {
        cfg.InfluxURL = os.Getenv("INFLUX_URL")
    }
    if v := os.Getenv("POLL_INTERVAL"); v != "" {
        if n, err := strconv.Atoi(v); err == nil && n > 0 {
            cfg.PollInterval = n
        }
    }

    // Validate mandatory URLs
    if cfg.OLHURL == "" || cfg.OLHHost == "" || cfg.InfluxURL == "" {
        return nil, errors.New("missing mandatory environment variables")
    }
    if ok, msg := isValidURL(cfg.OLHURL); !ok {
        return nil, fmt.Errorf("invalid OLHURL: %s", msg)
    }
    if ok, msg := isValidURL(cfg.InfluxURL); !ok {
        return nil, fmt.Errorf("invalid INFLUX_URL: %s", msg)
    }

    return cfg, nil
}

// godotenvLoad hides the import and error handling to make LoadConfig tidy.
func godotenvLoad() error {
    // Import here to avoid mandatory import if not used
    err := godotenv.Load()
    return err
}

// sanitizeString removes non‑word characters from a string.
func sanitizeString(s string) string {
    var b strings.Builder
    for _, r := range s {
        if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
            b.WriteRune(r)
        }
    }
    return b.String()
}
