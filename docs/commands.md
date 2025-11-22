# FAS Command Reference

List of commands supported by FAS (RESP-compatible).

## Protocol
- Primary: RESP Arrays of Bulk Strings (recommended for clients and pipelining).
- Legacy: Space-delimited inline commands are accepted for quick manual testing.
- GET on missing keys returns RESP Null (`$-1`).
- SUBSCRIBE replies and messages use RESP Arrays: `["subscribe", channel, count]`, `["message", channel, payload]`.

## 📦 Data Operations

### `SET`
Stores a string value for a key.
- **Syntax**: `SET key value`
- **Example**: `SET username wool`
- **Response**: `OK`

### `GET`
Retrieves the value stored at a key.
- **Syntax**: `GET key`
- **Example**: `GET username`
- **Response**: Bulk string value (e.g., `wool`) or RESP Null (`$-1`) if the key does not exist.
- **Note**: Returns `WRONGTYPE` error if the stored data type is not String.

---

### `PING`
- **Syntax**: `PING`
- **Response**: `PONG`

### `INFO`
- **Syntax**: `INFO`
- **Response** (bulk string): newline-separated stats like:
  ```
  keys:<count>
  keys_with_ttl:<count>
  expired:<count>
  memory_used:<bytes>
  max_memory:<bytes>
  ```

### `AUTH`
- **Syntax**: `AUTH password`
- **Response**: `OK` on success; `NOAUTH`/`ERR` otherwise.

### `SAVE`
- **Syntax**: `SAVE`
- **Response**: `OK` on success, error if snapshot path not configured.
- **Note**: Blocks the client while snapshot is written.

### `BGSAVE`
- **Syntax**: `BGSAVE`
- **Response**: `Background saving started` or error if one is already running.
- **Note**: Requires `-rdb` snapshot path. Runs in background.

---

## 📡 Messaging

### `SUBSCRIBE`
Subscribes to a specific channel to receive real-time messages.
- **Syntax**: `SUBSCRIBE channel`
- **Example**: `SUBSCRIBE notifications`
- **Behavior**: Upon execution, the connection switches to subscription mode and will continuously receive messages published to the channel.

### `PUBLISH`
Publishes a message to a specific channel.
- **Syntax**: `PUBLISH channel message`
- **Example**: `PUBLISH notifications "Hello everyone"`
- **Response**: `(integer) <number of subscribers received>`
