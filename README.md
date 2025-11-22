# FAS (Fast and Accurate System)

**FAS** stands for "Fast and Accurate Store". It is a high-performance in-memory data store and messaging system, similar to Redis but with a stronger focus on **Type Safety** and **Accuracy**.

## 🚀 Philosophy
- **Fast**: High throughput leveraging Go's concurrency.
- **Accurate**: Ensures data integrity through strict type checking.

## ✨ Key Features
- **In-Memory Key-Value Store**: Fast data storage and retrieval.
- **Type Safety**: Records data types at storage time and verifies them at retrieval time to prevent invalid operations.
- **Pub/Sub Messaging**: Real-time message publishing and subscription system.
- **Simple Protocol**: Intuitive text-based command protocol.

## 🛠 Getting Started

### Requirements
- Go 1.18+
- `nc` (Netcat) - For testing

### Running the Server
```bash
# Start the server
go run cmd/fas/main.go
```

### Usage (Client)
You can use the dedicated CLI tool `fs` to communicate with the server.

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

## 📚 Documentation
- [Architecture Guide](docs/architecture.md)
- [Command Reference](docs/commands.md)
- [Development Workflow](docs/workflow.md)

## 📄 License
This project is licensed under the **PolyForm Noncommercial License 1.0.0**.
You are free to use, modify, and distribute this software for **non-commercial purposes only**.
Commercial use is strictly prohibited without a separate commercial license.

