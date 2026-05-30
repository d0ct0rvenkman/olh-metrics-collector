package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	logrus "github.com/sirupsen/logrus"
)

// Client configuration will be loaded at runtime.
var cfg *ClientConfig
var verbose bool

// Load configuration once before main starts.
func init() {
	var err error
	cfg, err = LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		os.Exit(1)
	}
}

// Metric represents a single InfluxDB line‑protocol measurement.
type Metric struct {
	Measurement string
	Tags        map[string]string
	Fields      map[string]interface{}
	Timestamp   int64
}

// sanitizeLabel removes all non-word characters from the label.
func sanitizeLabel(label string) string {
	var b strings.Builder
	for _, r := range label {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isValidURL(u string) (bool, string) {
    // Remove surrounding quotes and whitespace
    u = strings.Trim(u, "\"'")
    u = strings.TrimSpace(u)
    if u == "" {
        return false, "URL is empty after trimming"
    }
    parsed, err := url.ParseRequestURI(u)
    if err != nil {
        return false, fmt.Sprintf("URL parse error: %v", err)
    }
    if parsed.Scheme != "http" && parsed.Scheme != "https" {
        return false, fmt.Sprintf("unsupported scheme: %s", parsed.Scheme)
    }
    return true, ""
}

// fetchCpuTemp gets the CPU temperature from the /api/cpuTemp/clean endpoint.
func fetchCpuTemp(ctx context.Context, cfg *ClientConfig) (float64, error) {
    // Skip fetching if host is local loopback (for tests)
    apiURL := cfg.OLHURL + "/api/cpuTemp/clean"
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
    if err != nil {
        return 0, err
    }
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return 0, err
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        return 0, fmt.Errorf("CPU temp API returned %s", resp.Status)
    }
    var r cpuTempResponse
    if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
        return 0, err
    }
    return r.Data, nil
}


func toLineProtocol(m Metric) string {
	var tagKeys, fieldKeys []string
	for k := range m.Tags {
		tagKeys = append(tagKeys, k)
	}
	for k := range m.Fields {
		fieldKeys = append(fieldKeys, k)
	}
	sort.Strings(tagKeys)
	sort.Strings(fieldKeys)
	var tagParts []string
	for _, k := range tagKeys {
		v := m.Tags[k]
		tagParts = append(tagParts, fmt.Sprintf("%s=%s", k, v))
	}
	var fieldParts []string
	for _, k := range fieldKeys {
		v := m.Fields[k]
		switch val := v.(type) {
		case float64:
			fieldParts = append(fieldParts, fmt.Sprintf("%s=%g", k, val))
		case int64:
			fieldParts = append(fieldParts, fmt.Sprintf("%s=%d", k, val))
		case int:
			fieldParts = append(fieldParts, fmt.Sprintf("%s=%d", k, val))
		case string:
			fieldParts = append(fieldParts, fmt.Sprintf("%s=%q", k, val))
		default:
			fieldParts = append(fieldParts, fmt.Sprintf("%s=%v", k, val))
		}
	}
	return fmt.Sprintf("%s,%s %s %d", m.Measurement, strings.Join(tagParts, ","), strings.Join(fieldParts, ","), m.Timestamp)

}

// Device represents a single entry inside the GetDevice.devices array.
// We only model the fields needed for this exporter.
type Device struct {
	IsTemperatureProbe bool    `json:"IsTemperatureProbe"`
	HasTemps           bool    `json:"HasTemps"`
	HasSpeed           bool    `json:"HasSpeed"`
	ProbeID            int     `json:"probeId"`
	Label              string  `json:"label"`
	DeviceID           string  `json:"deviceId"`
	Temperature        float64 `json:"temperature"`
	CPUTemp            float64 `json:"CpuTemp"`
	GPUTemp            float64 `json:"GpuTemp"`
	Description        string  `json:"description"`
	RPM                int64   `json:"rpm"`
}

// deviceGroup mirrors the nested JSON object structure.
type deviceGroup struct {
	GetDevice struct {
		Devices map[string]Device `json:"devices"`
		CpuTemp float64           `json:"CpuTemp"`
		GpuTemp float64           `json:"GpuTemp"`
	} `json:"GetDevice"`
}

// rootResponse is the top‑level JSON returned by the OpenLinkHub API.
type rootResponse struct {
    Devices map[string]deviceGroup `json:"devices"`
}

type cpuTempResponse struct {
    Code   int     `json:"code"`
    Status int     `json:"status"`
    Data   float64 `json:"data"`
}


func fetchMetrics(ctx context.Context, cfg *ClientConfig) ([]Metric, error) {
    // Fetch CPU temperature from separate API
    cpuTemp, err := fetchCpuTemp(ctx, cfg)
    if err != nil {
        logrus.Warnf("failed to fetch CPU temp: %v", err)
        cpuTemp = 0
    }

    req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.OLHURL+"/api/devices/", nil)

    if err != nil {
        return nil, err
    }
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("OpenLinkHub API returned %s", resp.Status)
    }

    var r rootResponse
    if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
        return nil, err
    }

    now := time.Now().Unix()
    var metrics []Metric
    for devID, group := range r.Devices {
        // Prepare tags for group-level metrics using first device

        for _, d := range group.GetDevice.Devices {
            tags := map[string]string{
                "host":   cfg.OLHHost,
                "device": devID,

                "probeID":    strconv.Itoa(d.ProbeID),
                "label":      sanitizeLabel(d.Label),
                "deviceType": d.Description,
                "deviceId":   d.DeviceID,
            }
            if d.IsTemperatureProbe || d.HasTemps {
                metrics = append(metrics, Metric{
                    Measurement: "openlinkhub",
                    Tags:        tags,
                    Fields:      map[string]interface{}{"temperature_c": d.Temperature},
                    Timestamp:   now,
                })
            }
            // Remove individual device CPU temp metric
            if d.GPUTemp != 0.0 {
                metrics = append(metrics, Metric{
                    Measurement: "openlinkhub",
                    Tags:        tags,
                    Fields:      map[string]interface{}{"gpu_temp_c": d.GPUTemp},
                    Timestamp:   now,
                })
            }
            if d.HasSpeed {
                metrics = append(metrics, Metric{
                    Measurement: "openlinkhub",
                    Tags:        tags,
                    Fields:      map[string]interface{}{"rpm": d.RPM},
                    Timestamp:   now,
                })
            }
        }
        // CPU temp emitted once after processing all groups
        if group.GetDevice.GpuTemp != 0.0 {
            metrics = append(metrics, Metric{
                Measurement: "openlinkhub",
                Tags:        map[string]string{"host": cfg.OLHHost, "device": devID},
                Fields:      map[string]interface{}{"gpu_temp_c": group.GetDevice.GpuTemp},
                Timestamp:   now,
            })
        }
    }
        // Emit CPU temp once after iterating all device groups
        if cpuTemp != 0 {
            metrics = append(metrics, Metric{
                Measurement: "openlinkhub",
                Tags:        map[string]string{"host": cfg.OLHHost},
                Fields:      map[string]interface{}{"cpu_temp_c": cpuTemp},
                Timestamp:   now,
            })
        }
        return metrics, nil
}


func postMetric(ctx context.Context, cfg *ClientConfig, line string) error {
	url := fmt.Sprintf("%s/api/v2/write?org=%s&bucket=%s&precision=s", cfg.InfluxURL, cfg.InfluxOrg, cfg.InfluxDB)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBufferString(line))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	req.Header.Set("Accept", "application/json")
	if cfg.InfluxToken != "" {
		req.Header.Set("Authorization", "Token "+cfg.InfluxToken)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("InfluxDB returned status %s", resp.Status)
}

func main() {
	flag.BoolVar(&verbose, "verbose", false, "enable verbose logging")
	flag.Parse()
	if verbose {
		logrus.SetLevel(logrus.InfoLevel)
	} else {
		logrus.SetLevel(logrus.ErrorLevel)
	}

	// Fallback to underscore-separated env vars if standard ones are missing
	// Validate URLs at startup.
	if valid, msg := isValidURL(cfg.OLHURL); !valid {
		logrus.Fatalf("invalid OLHURL: %s (%s)", cfg.OLHURL, msg)
	}
	if valid, msg := isValidURL(cfg.InfluxURL); !valid {
		logrus.Fatalf("invalid INFLUX_URL: %s (%s)", cfg.InfluxURL, msg)
	}

	// Configure logrus
	// Configure logrus: include timestamps only when run by a terminal
	// Configure logrus: include timestamps only when run by a terminal
	if os.Getenv("SYSTEMD_UNIT") == "" && os.Stdin != nil && (func() os.FileMode {
		fi, err := os.Stdin.Stat()
		if err != nil {
			return os.ModeCharDevice
		}
		return fi.Mode()
	}()&os.ModeCharDevice) != 0 {
		logrus.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})
	} else {
		logrus.SetFormatter(&logrus.TextFormatter{FullTimestamp: false})
	}
	logrus.Info("starting metrics exporter")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ticker := time.NewTicker(time.Duration(cfg.PollInterval) * time.Second)
	defer ticker.Stop()

	// initial fetch
	metrics, err := fetchMetrics(ctx, cfg)
	if err != nil {
		logrus.Errorf("failed to fetch metrics: %v", err)
	} else {
		for _, m := range metrics {
			line := toLineProtocol(m)
			logrus.Infof("posting: %s", line)
			if err := postMetric(ctx, cfg, line); err != nil {
				logrus.Errorf("failed to post metric: %v", err)
			} else {
				logrus.Info("posted metric successfully")
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			logrus.Info("shutting down")
			return
		case <-ticker.C:
			logrus.Info("starting metrics fetch")
			metrics, err := fetchMetrics(ctx, cfg)
			if err != nil {
				logrus.Errorf("failed to fetch metrics: %v", err)
				continue
			}
			logrus.Infof("fetched %d metrics", len(metrics))
			for _, m := range metrics {
				line := toLineProtocol(m)
				logrus.Infof("posting: %s", line)
				if err := postMetric(ctx, cfg, line); err != nil {
					logrus.Errorf("failed to post metric: %v", err)
				} else {
					logrus.Info("posted metric successfully")
				}
			}

		}
	}
}
