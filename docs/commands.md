# FAS Command Reference

List of commands supported by FAS.

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
- **Response**: Value (e.g., `wool`) or `(nil)` if the key does not exist.
- **Note**: Returns `WRONGTYPE` error if the stored data type is not String.

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
