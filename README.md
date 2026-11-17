# Go SCADA

A Go-based Supervisory Control and Data Acquisition (SCADA) system.

## Features

- Real-time in-memory tag database with pub/sub event bus
- Tag types: bool, int, float, string with quality and timestamps
- Driver framework with simulated process driver and Modbus TCP client driver
- Modbus TCP server (for device simulation/integration)
- Alarm manager with high/low thresholds, priorities, ack/clear
- Historical data storage in SQLite
- REST API + Server-Sent Events for live updates
- Embedded web HMI dashboard with live tags, alarms, trends

## Build

```bash
go build -o scada ./cmd/scada
```

## Run

```bash
./scada run -c config.json
```

Open http://localhost:8080

## Configuration

Edit `config.json` to define tags, alarms, and Modbus devices.

## Tests

```bash
go test ./...
```
