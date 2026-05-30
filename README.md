# OpenLinkHub InfluxDB Exporter

## Overview

This project collects device metrics from an **OpenLinkHub** instance and writes them to an InfluxDB database. It contacts the `/api/devices/` endpoint, parses the JSON payload, and converts the data into InfluxDB line‑protocol measurements.



All metrics carry the following tags:

* `host` – the `OLHHOST` value.
* `device` – the device group key returned by the API.
* `probeID` – the probe identifier.
* `label` – the sanitized device label.
* `deviceId` – the original device ID string.

The exporter validates the target URL, loads credentials from environment variables, 

---

## Dependencies

* Go 1.21 or later
* `github.com/sirupsen/logrus`
* `github.com/joho/godotenv`

All dependencies are fetched automatically via Go modules (`go mod tidy`).

---

## Build\n\n```bash\n# Clone the repository\ngit clone https://github.com/example/openlinkhub_exporter.git\ncd openlinkhub_exporter\n\n# Build the binary\ngo build -o olh_exporter\n```\n\nRun the binary directly; no server component.


---

## Configuration

The exporter accepts a handful of environment variables. The easiest way is to create a `.env` file.

```
OLHURL=https://localhost:27003
OLHHOST=$HOSTNAME
INFLUX_URL=http://localhost:8086
INFLUX_ORG=myorg
INFLUX_DB=mydb
INFLUX_TOKEN=secret
OLH_VERBOSE=false
```

Only OLHURL, OLHHOST, INFLUX_URL are mandatory. If `OLH_VERBOSE=true` the exporter will emit debug logs.

### Leading slash bug workaround
If an URL is missing a leading slash when combined with a path (`/api/devices/`) it can cause a missing “/” in the final URL, resulting in a 404. The exporter checks for this condition and will return a clear error.

---

## Running

```
# Using a .env file
export $(cat .env | xargs)   # or use `dotenv -i .env` if you have it installed
./olh_exporter
```



---

## Testing

Run the unit tests with:

```
go test ./...
```

The test suite verifies:

1. URL sanitisation and validation.
2. Correct processing of a sample `/api/devices/` payload.
3. Error handling for invalid inputs.

---

---

## Systemd

To run the exporter as a systemd service copy the provided unit file to `/etc/systemd/system/olh-metrics-collector.service` and the environment file to `/etc/olh-metrics-collector/config.env`:

```bash
sudo cp olh-metrics-collector.service /etc/systemd/system/
sudo mkdir -p /etc/olh-metrics-collector
sudo cp .env /etc/olh-metrics-collector/config.env
```

Edit `config.env` to set your environment variables (the variables are the same as those used by the `.env` file in the repository). Then enable and start the service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now olh-metrics-collector
```

The service will write logs to the system journal; view them with `journalctl -u olh-metrics-collector`.

(End of file - total 91 lines)



# Vibe Code Warning
This is absolutely vibe-coded/AI slop. I generally hate the idea of letting an autocorrect algorithm do my work, but this technology isn't going anywhere, so I need to be familiar with it. :\