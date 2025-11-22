# FAS (Fast and Accurate System)

**FAS** stands for "Fast and Accurate Store". It is a high-performance in-memory data store and messaging system, similar to Redis but with a stronger focus on **Type Safety** and **Accuracy**.

## 🚀 Philosophy
- **Fast**: High throughput leveraging Go's concurrency.
- **Accurate**: Ensures data integrity through strict type checking.
- **RESP-Compatible**: Uses a RESP-like protocol for binary-safe, pipelined commands.

## ✨ Key Features
- **In-Memory Key-Value Store**: Fast data storage and retrieval.
- **Type Safety**: Records data types at storage time and verifies them at retrieval time to prevent invalid operations.
- **Pub/Sub Messaging**: Real-time message publishing and subscription system.
- **Simple Protocol**: Intuitive text-based command protocol.
- **Persistence**: Append-only file (AOF) with configurable fsync policies.
- **Operations**: Built-in `PING` and `INFO` (memory/keys/expired) for basic health/monitoring.

## 🛠 Getting Started

### Requirements
- Go 1.18+
- `nc` (Netcat) - For testing

### Running the Server
```bash
# Start the server
go run cmd/fas/main.go

# Common flags
#   -host          (default localhost)
#   -port          (default 6379)
#   -aof           (default fas.aof)
#   -fsync         always|everysec|no (default everysec)
#   -eventloop     use single-threaded kqueue event loop (macOS only)
#   -maxmemory     bytes; FIFO eviction for non-TTL keys, TTL keys evicted if still over limit
#   -requirepass   optional password; clients must AUTH <password>
#   -auth          enable AUTH requirement (must be true to enforce password)
#   -tls-cert/-tls-key  enable TLS listener
#   -allow-cidr    comma-separated CIDR allowlist (standard mode only)
#   -rdb           path to snapshot file for SAVE/BGSAVE (loaded before AOF if present)
```

### Usage (Client)
`fas` = server, `fs` = client.

Quick start (no auth):
```bash
go run cmd/fas/main.go
go run cmd/fs/main.go PING
```

Install binaries to PATH:
```bash
make install                     # installs fas and fs to /usr/local/bin
fas -h                           # run server from PATH
fs PING                          # client command from PATH
```

Common client flags:
- `-host` (default 127.0.0.1)
- `-port` (default 6379)
- `-tls` (connect with TLS, skips verification)
- `FAS_PASSWORD` env: auto `AUTH <password>` before first command

### Examples
**Run server**
```bash
# No auth, no TLS
go run cmd/fas/main.go

# With auth + snapshot + AOF + TLS
go run cmd/fas/main.go \
  -auth -requirepass secret123 \
  -rdb /var/lib/fas/fas.rdb \
  -aof /var/lib/fas/fas.aof \
  -fsync everysec \
  -tls-cert /path/to/cert.pem \
  -tls-key /path/to/key.pem
```

**Client single command**
```bash
go run cmd/fs/main.go -host 127.0.0.1 -port 6379 PING

# With password (auto AUTH)
FAS_PASSWORD=secret123 go run cmd/fs/main.go GET mykey

# With TLS
go run cmd/fs/main.go -tls PING
```

**Client interactive (REPL)**
```bash
go run cmd/fs/main.go                # enter commands: PING, SET k v, GET k
FAS_PASSWORD=secret123 go run cmd/fs/main.go -tls   # auth + TLS, then type commands
```

**1. Data Operations**
```bash
# Set data (SET key value)
go run cmd/fs/main.go SET mykey hello_world

# Get data (GET key)
go run cmd/fs/main.go GET mykey

# PING (health)
go run cmd/fs/main.go PING

# INFO (stats)
go run cmd/fs/main.go INFO

# Authenticate (if server started with -auth -requirepass)
FAS_PASSWORD=secret go run cmd/fs/main.go GET mykey   # auto AUTH via env
go run cmd/fs/main.go AUTH secret
go run cmd/fs/main.go GET mykey

# Snapshot (requires -rdb)
go run cmd/fs/main.go SAVE
go run cmd/fs/main.go BGSAVE
```

**2. Pub/Sub (Messaging)**
*Terminal 1 (Subscriber)*
```bash
go run cmd/fs/main.go SUBSCRIBE news
```

*Terminal 2 (Publisher)*
```bash
go run cmd/fs/main.go PUBLISH news breaking_news!
```

### RESP Examples (manual)
```
*3\r\n$3\r\nSET\r\n$3\r\nkey\r\n$5\r\nvalue\r\n
*2\r\n$3\r\nGET\r\n$3\r\nkey\r\n
*1\r\n$4\r\nPING\r\n
```

### Monitoring/Health
- `PING` → `PONG`
- `INFO` (newline-separated fields): `keys:<n>`, `keys_with_ttl:<n>`, `expired:<n>`, `memory_used:<bytes>`, `max_memory:<bytes>`
  ```bash
  go run cmd/fs/main.go INFO
  ```

## 🧪 Testing
We use Go's built-in testing framework.

### Running Tests
To run all tests:
```bash
go test ./test/...
```

### Verbose Output
To see detailed step-by-step logs of what each test is doing:
```bash
go test -v ./test/...
```

## 📚 Documentation
- [Architecture Guide](docs/architecture.md)
- [Command Reference](docs/commands.md)
- [Development Workflow](docs/workflow.md)

## 📄 License
This project is licensed under the **PolyForm Noncommercial License 1.0.0**.
You are free to use, modify, and distribute this software for **non-commercial purposes only**.
Commercial use is strictly prohibited without a separate commercial license.
