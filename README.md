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
A dedicated CLI is under development. Currently, you can use `nc` to communicate with the server.

**1. Data Operations**
```bash
# Set data (SET key value)
echo "SET mykey hello_world" | nc localhost 6379

# Get data (GET key)
echo "GET mykey" | nc localhost 6379
```

**2. Pub/Sub (Messaging)**
*Terminal 1 (Subscriber)*
```bash
nc localhost 6379
SUBSCRIBE news
```

*Terminal 2 (Publisher)*
```bash
echo "PUBLISH news breaking_news!" | nc localhost 6379
```

## 📚 Documentation
- [Architecture Guide](docs/architecture.md)
- [Command Reference](docs/commands.md)
