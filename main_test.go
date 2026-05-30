package main

import (
    "bytes"
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "os"
    "testing"
    "fmt"
    "strings"

)

func TestSanitizeLabel(t *testing.T) {
	cases := map[string]string{
		"normal":      "normal",
		"with spaces": "withspaces",
		"special!#%":  "special",
		"mixed_123":   "mixed_123",
	}
	for input, want := range cases {
		got := sanitizeLabel(input)
		if got != want {
			t.Errorf("sanitizeLabel(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestToLineProtocol(t *testing.T) {
	m := Metric{Measurement: "openlinkhub", Tags: map[string]string{"host": "core.gcl"}, Fields: map[string]interface{}{"temperature_c": 23.5}, Timestamp: 1234567890}
	want := "openlinkhub,host=core.gcl temperature_c=23.5 1234567890"
	got := toLineProtocol(m)
	if got != want {
		t.Fatalf("toLineProtocol = %q, want %q", got, want)
	}
}

// mock API server that returns a minimal valid JSON structure.
func mockAPIResponse(t *testing.T, deviceID string, devices []Device) *httptest.Server {
    // Convert slice to map with numeric keys as strings
    deviceMap := make(map[string]Device)
    for i, d := range devices {
        deviceMap[fmt.Sprintf("%d", i)] = d
    }
    getDevice := struct{ Devices map[string]Device `json:"devices"`; CpuTemp float64 `json:"CpuTemp"`; GpuTemp float64 `json:"GpuTemp"` }{Devices: deviceMap}
    data := rootResponse{Devices: map[string]deviceGroup{deviceID: deviceGroup{GetDevice: getDevice}}}
    devBuf := &bytes.Buffer{}
    if err := json.NewEncoder(devBuf).Encode(data); err != nil {
        t.Fatalf("encoding failed: %v", err)
    }
    // CPU temp response
    cpuResp := map[string]interface{}{"code": 200, "status": 1, "data": 34.0}
    cpuBuf := &bytes.Buffer{}
    if err := json.NewEncoder(cpuBuf).Encode(cpuResp); err != nil {
        t.Fatalf("encoding cpu temp failed: %v", err)
    }
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if strings.HasSuffix(r.URL.Path, "/api/devices/") {
            w.WriteHeader(http.StatusOK)
            w.Write(devBuf.Bytes())
        } else if strings.HasSuffix(r.URL.Path, "/api/cpuTemp/clean") {
            w.WriteHeader(http.StatusOK)
            w.Write(cpuBuf.Bytes())
        } else {
            http.NotFound(w, r)
        }
    }))
    return srv
}


func TestFetchMetrics(t *testing.T) {
    // Prepare mock devices.
    devs := []Device{{IsTemperatureProbe: true, ProbeID: 1, Label: "TMP 1", DeviceID: "dev1", Temperature: 20.0}}
    srv := mockAPIResponse(t, "e20510308a4884bac487c3261091005f", devs)
    defer srv.Close()
    os.Setenv("OLHURL", srv.URL)
    os.Setenv("INFLUX_URL", "http://dummy")
    os.Setenv("OLHHOST", "demohost")
    cfg, err := LoadConfig()
    if err != nil {
        t.Fatalf("LoadConfig failed: %v", err)
    }
    metrics, err := fetchMetrics(context.Background(), cfg)

    if err != nil {
        t.Fatalf("fetchMetrics returned error: %v", err)
    }
    if len(metrics) != 2 {
        t.Fatalf("expected 2 metrics, got %d", len(metrics))
    }
    var tempMetric, cpuMetric Metric
    for _, m := range metrics {
        if _, ok := m.Fields["temperature_c"]; ok {
            tempMetric = m
        } else if _, ok := m.Fields["cpu_temp_c"]; ok {
            cpuMetric = m
        }
    }
    if tempMetric.Fields["temperature_c"] != 20.0 {
        t.Errorf("expected temperature 20.0, got %v", tempMetric.Fields["temperature_c"])
    }
    if cpuMetric.Fields["cpu_temp_c"] != 34.0 {
        t.Errorf("expected cpu temp 34.0, got %v", cpuMetric.Fields["cpu_temp_c"])
    }

}
