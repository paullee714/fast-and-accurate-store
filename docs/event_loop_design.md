# Single Threaded Event Loop Architecture Design

## Goal
Transition from a Multi-Threaded (Goroutine-per-connection) model to a **Single Threaded Event Loop** model to minimize context switching overhead and lock contention, aligning with the "Fast and Accurate" philosophy.

## Core Concept
Instead of spawning a goroutine for each connection, we will use a single main thread (goroutine) that monitors multiple file descriptors (sockets) using the OS's non-blocking I/O notification mechanism (**kqueue** on macOS, **epoll** on Linux).

## Components

### 1. Event Loop (`EventLoop`)
- **Mechanism**: Uses `syscall.Kqueue` (macOS) or `syscall.Epoll` (Linux).
- **Responsibility**:
    - Registers the listener socket to wake up on new connections (`Accept`).
    - Registers client sockets to wake up when data is readable (`Read`).
    - Dispatches events to handlers.
- **Single Threaded**: All logic runs in this loop. No mutexes needed for `Store` or `PubSub` as there is no concurrent access.

### 2. Non-Blocking I/O
- **Accept**: When the listener is ready, call `syscall.Accept` (or `net.Listener.Accept` with non-blocking fd).
- **Read**: When a client socket is readable, read data into a buffer.
    - If data forms a complete command, execute it immediately.
    - If partial, buffer it.
- **Write**: Write responses directly to the non-blocking socket. If the kernel buffer is full, buffer the rest and register for `Write` events (optional for MVP, can block slightly or drop).

### 3. Lock-Free Data Structures
- Since execution is serialized on the main thread, `sync.RWMutex` in `Store` and `PubSub` can be **removed**.
- This eliminates lock contention entirely.

## Implementation Details (macOS/Kqueue)

### Dependencies
- We will use `golang.org/x/sys/unix` for cleaner syscall access if possible, or standard `syscall` if we want zero-dependency (though `syscall` is frozen/deprecated, it works).
- **Decision**: Use `syscall` for zero-dependency MVP, or `golang.org/x/sys/unix` if we decide to add a dependency. Given the "Fast and Accurate" goal, `unix` package is safer.

### Flow
1.  **Start**: Create Kqueue.
2.  **Listen**: Create TCP socket, bind, listen, set non-blocking. Register `EVFILT_READ` for listener fd.
3.  **Loop**: Call `Kevevent` to wait for events.
4.  **Event**:
    - If `ident == listenerFd`: Loop `Accept` until EAGAIN. Register new client fds with `EVFILT_READ`.
    - If `ident == clientFd`: `Read` into buffer.
        - Parse RESP.
        - Execute Command.
        - `Write` response.
        - If `Read` returns 0 (EOF), close connection.

## Limitations
- **CPU Bound**: Since it's single-threaded, CPU-intensive commands (like `KEYS *` on huge datasets) will block the entire server. (Redis has this too).
- **Blocking I/O**: Disk I/O (AOF fsync) must be handled carefully. `fsync` should be done in a separate thread (already implemented in background sync) to avoid blocking the event loop.

## Migration Plan
1.  Create `pkg/server/eventloop_darwin.go` (for Mac implementation).
2.  Refactor `Store` and `PubSub` to allow "unsafe" (lock-free) access or just remove locks if we guarantee single-threaded usage.
3.  Update `main.go` to start the Event Loop server instead of the Goroutine server.
