# FAS Technical Overview & Usage

## Core Features
- RESP-compatible TCP server with optional auth, TLS (standard mode), CIDR allowlist, maxclients, and maxmemory policies.
- Storage: in-memory map with TTL and adaptive expiration; eviction policies: `allkeys-random` (default), `volatile-random`, `allkeys-lru`, `allkeys-lfu`, `no-eviction`. LRU/LFU use access timestamp/frequency with min-heaps (O(log n) victim selection, stale-safe).
- Persistence: AOF (fsync policy: always/everysec/no), RDB snapshots (`-rdb`), commands `SAVE` (sync) / `BGSAVE` (async), AOF replay after snapshot load on startup.
- Pub/Sub: RESP arrays for SUBSCRIBE/messages.
- Monitoring/Ops: `PING`, `INFO` (keys, keys_with_ttl, expired, memory_used, max_memory), optional `/metrics` (Prometheus-style) via `-metrics-port`.
- Config: CLI flags + optional key=value config file (`-config`) that overrides flags.
- Clients: `fs` CLI with `-host`, `-port`, `-tls` (insecure), `FAS_PASSWORD` auto-AUTH; RESP-compatible for other clients.

## Server CLI (fas)
Flags:
- `-host` (default `localhost`)
- `-port` (default `6379`)
- `-aof` (default `fas.aof`)
- `-fsync` (`always|everysec|no`, default `everysec`)
- `-eventloop` (single-thread kqueue/epoll; TLS/CIDR unsupported in this mode)
- `-maxmemory` (bytes; default 1GB)
- `-maxmemory-policy` (`allkeys-random|volatile-random|allkeys-lru|allkeys-lfu|noeject`)
- `-auth` (require AUTH)
- `-requirepass` (password when `-auth` is true)
- `-tls-cert`, `-tls-key` (standard mode only)
- `-allow-cidr` (comma-separated allowlist, standard mode only)
- `-rdb` (snapshot path; loaded before AOF)
- `-maxclients` (0 = unlimited)
- `-config` (key=value file; overrides)
- `-metrics-port` (Prometheus `/metrics` on this port, 0 = disabled)

Start examples:
```bash
# No auth
fas -host 0.0.0.0 -port 6379

# Auth + persistence + TLS + metrics
fas -host 0.0.0.0 -port 6379 \
  -auth -requirepass secret123 \
  -aof /var/lib/fas/fas.aof \
  -rdb /var/lib/fas/fas.rdb \
  -fsync everysec \
  -maxmemory 1073741824 \
  -maxmemory-policy allkeys-lru \
  -tls-cert /path/cert.pem -tls-key /path/key.pem \
  -metrics-port 9100
```

## Client CLI (fs)
Flags:
- `-host` (default `127.0.0.1`)
- `-port` (default `6379`)
- `-tls` (connect with TLS, skips verify)
- `FAS_PASSWORD` env: auto `AUTH <password>` before first command.

Usage examples:
```bash
fs PING
fs SET k v
fs GET k
# Auth
FAS_PASSWORD=secret fs GET k
# TLS
fs -tls PING
# Interactive REPL
fs                  # then type commands; quit/exit to leave
```

## Commands (RESP arrays; inline also accepted)
- `AUTH <password>` → `OK` or `NOAUTH/ERR`
- `PING` → `PONG` (bulk)
- `INFO` → bulk lines:
  - `keys:<n>`
  - `keys_with_ttl:<n>`
  - `expired:<n>`
  - `memory_used:<bytes>`
  - `max_memory:<bytes>`
- `SET <key> <value>` → `OK`
- `GET <key>` → bulk value or `$-1` if missing; `WRONGTYPE` on type error
- `PUBLISH <channel> <message>` → `:count`
- `SUBSCRIBE <channel>` → `["subscribe", channel, count]`, then messages `["message", channel, payload]`
- `SAVE` → `OK` (requires `-rdb`)
- `BGSAVE` → `Background saving started` (error if already running or `-rdb` unset)

## Config File (key=value)
Example `/etc/fas/fas.conf`:
```
host=0.0.0.0
port=6379
aof=/var/lib/fas/fas.aof
rdb=/var/lib/fas/fas.rdb
fsync=everysec
auth=true
requirepass=secret123
maxmemory=1073741824
maxmemory-policy=allkeys-lru
maxclients=10000
metrics-port=9100
```
Load via `-config /etc/fas/fas.conf`.

## Metrics (/metrics when -metrics-port set)
Exposes Prometheus-style text:
- `fas_keys`
- `fas_keys_with_ttl`
- `fas_expired_total`
- `fas_memory_used_bytes`
- `fas_max_memory_bytes`
- `fas_clients`

## Event Loop Modes
- Standard (goroutine per connection): supports TLS, CIDR, auth, metrics.
- Event loop (macOS kqueue / Linux epoll): single-threaded; TLS/CIDR unsupported; loads RDB/AOF; metrics enabled if configured.

## Persistence
- Startup: load RDB (`-rdb`) if present, then AOF replay (`-aof`).
- AOF: flush on every write; fsync per policy.
- Snapshot: `SAVE` (sync) / `BGSAVE` (async) write RDB with atomic rename.

## Eviction
- Policies: `allkeys-random`, `volatile-random`, `allkeys-lru`, `allkeys-lfu`, `no-eviction`.
- LRU/LFU: per-item `LastAccess`/`Freq` with versioned min-heaps for O(log n) eviction; TTL keys can be evicted if still over limit; FIFO for non-TTL keys in default path.

## Build/Install
- `make build` (fas, fs into `bin/`)
- `make test`
- `make install` (installs fas/fs to `/usr/local/bin`)
- `scripts/start-server.sh [--auth pwd] [extra fas flags]` (auto-build if missing)

## Tests
- Unit: protocol parsing, pubsub, store (TTL/type safety/not found/eviction policies).
- Functional: CRUD (PING/SET/GET), Pub/Sub, TTL expiry via server command path.
- Integration: builds `fas`, starts server, verifies PING/SET/GET and Pub/Sub end-to-end.

## Limitations / Notes
- Event loop mode: no TLS/CIDR.
- No multi-key/transaction commands yet.
- LRU/LFU approximate (heap-based; no decay).
- `fs` TLS skips certificate verification.
