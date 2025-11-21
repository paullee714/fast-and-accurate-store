# FAS Architecture

System architecture documentation for FAS (Fast and Accurate System).

## 🏗 System Overview

```mermaid
graph TD
    Client[Client (fs CLI)] -->|TCP Connection| Server[TCP Server]
    Server -->|Stream| Parser[Protocol Parser]
    Parser -->|Command Object| Executor[Command Executor]
    Executor -->|Read/Write| Store[In-Memory Store]
    Executor -->|Pub/Sub| PubSub[Messaging System]
```

### 1. TCP Server (`pkg/server`)
- Accepts and manages client connections.
- Each connection is handled in a separate Goroutine.
- Implemented using the standard `net` package.

### 2. Protocol Parser (`pkg/protocol`)
- Parses incoming byte streams into command objects (`Command`).
- Currently uses a space-delimited text protocol.
    - Example: `SET key value` -> `Command{Name: "SET", Args: ["key", "value"]}`

### 3. In-Memory Store (`pkg/store`)
- **Type Safety**: All data is wrapped in an `Item` struct, which includes `DataType` information.
- **Concurrency Control**: Uses `sync.RWMutex` to protect data in a thread-safe manner.
- **Data Structure**:
    ```go
    type Item struct {
        Value     interface{} // Actual data
        Type      DataType    // Data type (String, List, etc.)
        ExpiresAt int64       // Expiration timestamp
    }
    ```

### 4. Messaging System (`pkg/pubsub`)
- Supports channel-based Publish/Subscribe model.
- **Subscribe**: When a client subscribes to a channel, the connection switches to a message reception-only mode.
- **Publish**: Publishing a message to a channel broadcasts it to all clients currently subscribing to that channel.
