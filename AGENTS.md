# AGENTS.md

This file helps coding agents (Claude, Cursor, Codex, etc.) understand the mineSQL codebase and work effectively within it.

---

## Project Overview

mineSQL is a Postgres-wire-compatible relational database engine written in Go. Its storage backend is a live Minecraft world — every row is a strip of banner blocks and wall signs, every chunk is a page, every Y level is a table. It implements real database internals: WAL, MVCC, a query executor, and a SQL parser.

Full technical spec (authoritative): [`docs/spec.md`](./docs/spec.md)

**When in doubt about any decision — build order, wire protocol messages, WAL invariants, row encoding format, plugin protocol opcodes — the spec wins.**

The Paper plugin (Java, separate repo) lives at: `github.com/swapnil404/minesql-plugin`

---

## Repository Layout

```
minesql/
├── cmd/minesql/         # main entrypoint — starts wire server, initializes HAL
├── internal/
│   ├── wire/            # Postgres wire protocol (jackc/pgproto3)
│   ├── parser/          # SQL parsing (pganalyze/pg_query_go)
│   ├── planner/         # query planner — produces execution plans
│   ├── executor/        # pull-based query executor
│   ├── storage/         # row encoding, page/chunk layout, table metadata
│   ├── wal/             # write-ahead log, crash recovery
│   ├── mvcc/            # transaction manager, xmin/xmax visibility
│   └── hal/             # Minecraft HAL — TCP client to Paper plugin
├── docker/
│   ├── Dockerfile       # Paper server image with plugin pre-installed
│   └── world/           # pre-seeded world (WAL region + control block)
├── docker-compose.yml
├── docker-compose.dev.yml
└── docs/
    └── architecture.md
```

---

## Build & Run

```bash
# Build
go build ./cmd/minesql

# Run (requires Minecraft server running, see docker-compose.dev.yml)
go run ./cmd/minesql

# Run all tests
go test ./...

# Run tests for a specific package
go test ./internal/wal/...

# Lint
golangci-lint run
```

### Dev Environment

Start Minecraft only (engine runs locally):
```bash
docker compose -f docker-compose.dev.yml up
```

Full stack:
```bash
docker compose up
```

---

## Code Style

- **Formatter**: `gofmt` — all code must be gofmt clean, no exceptions
- **Linter**: `golangci-lint` — run before any commit
- **Error handling**: always handle errors explicitly, never `_` an error unless there is a clear documented reason
- **No global state**: pass dependencies explicitly, do not use package-level variables for mutable state
- **Interfaces over concrete types**: especially at package boundaries (e.g. `hal.Storage`, not `hal.MinecraftHAL` directly)
- **Context propagation**: all blocking operations (HAL calls, TCP reads) must accept and respect `context.Context`
- **Package naming**: short, lowercase, no underscores — `wal`, `mvcc`, `hal`, `wire`

---

## Layer Responsibilities

Understanding which layer owns what prevents putting logic in the wrong place:

| Layer | Owns | Does NOT own |
|---|---|---|
| `wire` | Postgres protocol framing, message types | SQL parsing, query logic |
| `parser` | AST construction from SQL string | planning, execution |
| `planner` | Choosing scan strategy, plan tree | executing plans |
| `executor` | Iterating over plan, producing rows | storage format, Minecraft |
| `storage` | Row encoding, page layout, table metadata | WAL, MVCC, Minecraft |
| `wal` | Log entry write/read, crash recovery | storage format details |
| `mvcc` | Transaction IDs, xmin/xmax visibility | row encoding |
| `hal` | TCP connection to plugin, block I/O | everything above |

**Critical rule**: the `hal` package must never import any other internal package. It is the bottom of the dependency graph. If you find yourself importing `storage` from `hal`, something is wrong.

---

## Storage Format

Rows use a hybrid banner+sign layout. Each row occupies a fixed-width strip of blocks along X at a fixed (Y, Z):

- **Banners 0–1**: xmin and xmax (int64, 8 bytes each, encoded as heraldic pattern layers). Each of a banner's 6 pattern layers encodes 1 byte: high nibble = pattern type (0–15), low nibble = dye color (0–15).
- **Banners 2..N**: INT / BIGINT / BOOLEAN columns (6 bytes per banner, packed in ordinal order). INT is 4 bytes (1 banner). BIGINT is 8 bytes (2 banners).
- **Signs 0..M**: TEXT columns. One OAK_WALL_SIGN per TEXT column (4 lines × 16 chars = 64 chars). Longer TEXT chains multiple signs.

Null sentinels: `0xFF` bytes in banner layers for numeric nulls; `"\x00NULL\x00"` on sign line 0 for TEXT nulls.

WAL entries are lecterns holding written books. Each lectern is at X ≥ 100000, Y=64, Z sequentially. Book pages encode LSN, TXID, status, operation, target coordinates, and a new-value summary. Walking up to a lectern and opening it shows the raw transaction log.

The catalog table (table 0, Y=64) and control block at (0, 64, 0) use the same banner+sign encoding.

---

## The HAL Interface

All Minecraft I/O goes through this interface. Do not call the plugin TCP connection directly from outside the `hal` package:

```go
type Storage interface {
    ReadBlock(ctx context.Context, x, y, z int) ([]byte, error)
    WriteBlock(ctx context.Context, x, y, z int, blockType string, data []byte) error
    BatchRead(ctx context.Context, positions []BlockPos) ([][]byte, error)
    ForceLoadChunk(ctx context.Context, chunkX, chunkZ int) error
}
```

`blockType` is `"banner"`, `"sign"`, or `"lectern"` — the HAL uses it to select the correct plugin protocol opcode variant.

---

## WAL Rules

These are invariants. Never break them:

1. **WAL entry must be written and ACKed before the data block write begins**
2. **WAL entry must be marked COMMITTED only after the data block write is ACKed**
3. **On startup, WAL recovery must complete before the wire server accepts connections**
4. **Never truncate a WAL entry that is still PENDING**

---

## MVCC Rules

- Every row written by the storage layer must have `xmin` set to the current transaction ID and `xmax` set to null
- A DELETE sets `xmax` on the existing row — it never removes the block
- An UPDATE is always a DELETE + INSERT — never mutate a row in place
- A SELECT must filter rows through the MVCC visibility check before returning them
- The transaction ID counter is persisted in the control block at `(0, 64, 0)` — always read it on startup, never assume it starts at 1

---

## Testing

- Unit tests live alongside the package they test (`internal/wal/wal_test.go`)
- Integration tests that require a live Minecraft connection are in `internal/hal/integration_test.go` and are skipped unless `MINESQL_MINECRAFT_ADDR` is set
- When writing tests for the storage layer, use the `hal/mock` package — never require a real Minecraft server for unit tests
- Test table names should use `t.Name()` to avoid collisions between parallel tests

---

## Security Considerations

- The plugin TCP server (port 25576) must never be exposed to the public internet — it has no authentication. It is internal only.
- The Postgres wire server (port 5432) has no authentication in v1 — document this clearly, do not silently accept any username/password
- Never log row data at INFO level — it may contain sensitive values. Use DEBUG only.
- The Minecraft RCON port (25575) should be disabled in `server.properties` — mineSQL uses the plugin protocol exclusively

---

## Common Pitfalls

- **Chunk not loaded**: if a HAL read returns empty data unexpectedly, check whether the target chunk is forceloaded. Call `ForceLoadChunk` before any table scan.
- **WAL region overlap**: the WAL region starts at X=100000. Never place table data near this coordinate.
- **txid counter on restart**: always read the control block on startup and add a safety margin (+100) before assigning new transaction IDs.
- **RCON vs plugin**: do not use RCON for any data operations. RCON is single-threaded and will deadlock under load. The plugin TCP server is the only I/O path.
- **Banner encoding**: each banner encodes exactly 6 bytes. INT is 4 bytes (1 banner, 2 bytes wasted). BIGINT is 8 bytes (2 banners). TEXT uses wall signs not banners. Never mix encoding types for a column.
