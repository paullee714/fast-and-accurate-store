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
```

### Usage (Client)
You can use the dedicated CLI tool `fs` (RESP) to communicate with the server.

**1. Data Operations**
```bash
# Set data (SET key value)
go run cmd/fs/main.go SET mykey hello_world

# Get data (GET key)
go run cmd/fs/main.go GET mykey
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
