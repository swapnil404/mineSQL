# mineSQL — Technical Specification

This document is the authoritative record of every architectural decision made for mineSQL. All implementation must conform to this spec. When in doubt, this document wins.

---

## Table of Contents

1. [Project Identity](#1-project-identity)
2. [Technology Stack](#2-technology-stack)
3. [Repository Structure](#3-repository-structure)
4. [Build Order](#4-build-order)
5. [Postgres Wire Protocol](#5-postgres-wire-protocol)
6. [SQL Parser](#6-sql-parser)
7. [Query Planner and Executor](#7-query-planner-and-executor)
8. [Storage Layout](#8-storage-layout)
9. [Row Encoding](#9-row-encoding)
10. [Hardware Abstraction Layer (HAL)](#10-hardware-abstraction-layer-hal)
11. [Plugin Binary Protocol](#11-plugin-binary-protocol)
12. [Write-Ahead Log (WAL)](#12-write-ahead-log-wal)
13. [MVCC](#13-mvcc)
14. [Transaction ID Counter](#14-transaction-id-counter)
15. [Vacuum](#15-vacuum)
16. [Docker Setup](#16-docker-setup)
17. [v1 Scope](#17-v1-scope)
18. [v2 Scope](#18-v2-scope)
19. [Minecraft Chat Interface](#19-minecraft-chat-interface)

---

## 1. Project Identity

**Name**: mineSQL
**Tagline**: A relational database engine that uses a Minecraft world as its physical storage backend.
**Module**: `github.com/swapnil404/minesql`
**Author**: swapnil404
**Language**: Go (engine), Java (Paper plugin)
**Plugin repo**: `github.com/swapnil404/minesql-plugin`
**License**: MIT

The name is intentional. Like MySQL and PostgreSQL, the SQL suffix signals what it is. The Mine prefix signals where it stores data.

---

## 2. Technology Stack

| Component | Decision | Reason |
|---|---|---|
| Engine language | Go | Good concurrency primitives, fast compile, strong networking stdlib, manageable for a solo developer |
| Wire protocol library | `jackc/pgproto3` | Handles all Postgres protocol framing, avoids implementing binary encoding from scratch |
| SQL parser | `pganalyze/pg_query_go` | Wraps the actual Postgres parser, returns a full AST, no hand-rolled parser |
| Plugin platform | Paper (latest stable) | Best API for safe server-thread scheduling, active maintenance, widely used |
| Plugin language | Java 21 | Required by Paper, standard for Minecraft plugin development |
| Minecraft version | Latest stable at time of build | Pin in Dockerfile |
| Container | Docker + Docker Compose | One-command setup for demos and contributors |
| Row serialization | Hybrid banner+sign encoding | INT/BIGINT/BOOLEAN as banner pattern bytes, TEXT as sign lines, WAL as lectern books — visually inspectable in-game |

**Language choice rationale**: Go was chosen over Rust. The project involves real DB internals, Minecraft I/O, and a Postgres wire protocol implementation simultaneously. Rust would significantly slow early development. Go is the right tradeoff for a solo developer learning DB internals at the same time as building.

---

## 3. Repository Structure

```
minesql/
├── cmd/
│   └── minesql/
│       └── main.go          # entrypoint — initializes HAL, starts wire server
├── internal/
│   ├── wire/                # Postgres wire protocol server
│   ├── parser/              # SQL parser integration (pg_query_go wrapper)
│   ├── planner/             # query planner — produces plan trees
│   ├── executor/            # pull-based query executor
│   ├── storage/             # row encoding, page layout, table metadata, free space
│   ├── wal/                 # write-ahead log, crash recovery
│   ├── mvcc/                # transaction manager, xmin/xmax visibility rules
│   └── hal/
│       ├── hal.go           # Storage interface definition
│       ├── client.go        # TCP client to Paper plugin
│       ├── mock/            # mock HAL for unit tests
│       └── integration_test.go  # skipped unless MINESQL_MINECRAFT_ADDR is set
├── docker/
│   ├── Dockerfile           # Paper server + minesql-plugin pre-installed
│   └── world/               # pre-seeded world with WAL region + control block
├── docs/
│   ├── spec.md              # this file
│   └── architecture.md      # prose explanation of all concepts
├── docker-compose.yml       # full stack
├── docker-compose.dev.yml   # Minecraft only
├── AGENTS.md
└── README.md
```

---

## 4. Build Order

This is the order in which layers must be built. Each layer depends on the one below it being complete. Do not skip ahead.

**Phase 1 — Foundation (target: June 22)**

1. **Wire Protocol** — get `psql` to connect and return a hardcoded row. No SQL, no storage. Just the handshake and a fake `SELECT 1` response. (~150 lines)
2. **HAL + Plugin** — Paper plugin TCP server, Go HAL client. Verified working: `WriteBlock` then `ReadBlock` round-trips correctly. No database logic.

**Phase 2 — Core Engine**

3. **SQL Parser** — integrate `pg_query_go`, parse `SELECT`, `INSERT`, `CREATE TABLE` into AST nodes.
4. **Storage Layer** — row encoding, block coordinate math, free space tracking, table metadata.
5. **Query Executor** — seq scan, INSERT execution, WHERE filter.
6. **WAL** — write-ahead log, crash recovery on startup.
7. **MVCC** — transaction IDs, visibility rules, concurrent transaction support.

**Phase 3 — Polish**

8. **Docker** — full compose setup, pre-seeded world, one-command demo.
9. **Vacuum** — background goroutine, dead row cleanup.

---

## 5. Postgres Wire Protocol

**Port**: 5432
**Library**: `jackc/pgproto3`

### Connection lifecycle

```
Client → StartupMessage { user, database }
Server → AuthenticationOk
Server → ParameterStatus (server_version = "15.0")
Server → ParameterStatus (client_encoding = "UTF8")
Server → ParameterStatus (standard_conforming_strings = "on")
Server → BackendKeyData { pid=1, secret=0 }
Server → ReadyForQuery { status='I' }

-- query loop --
Client → Query { sql string }
Server → RowDescription (for SELECT)
Server → DataRow × N
Server → CommandComplete
Server → ReadyForQuery { status='I' }

Client → Terminate
Server → (close connection)
```

### Supported message types (v1)

Inbound: `StartupMessage`, `Query`, `Terminate`
Outbound: `AuthenticationOk`, `ParameterStatus`, `BackendKeyData`, `ReadyForQuery`, `RowDescription`, `DataRow`, `CommandComplete`, `ErrorResponse`

### Error handling

All errors use `ErrorResponse` with:
- `Severity`: ERROR
- `Code`: appropriate SQLSTATE (e.g. `42P01` for undefined table, `42601` for syntax error)
- `Message`: human-readable description

### v1 limitations

- No authentication. Any username/password is accepted.
- No TLS.
- No extended query protocol (prepared statements). Simple query protocol only.
- Single connection at a time is acceptable for v1 but goroutine-per-connection should be implemented from the start.

---

## 6. SQL Parser

**Library**: `pganalyze/pg_query_go`

This wraps the actual Postgres C parser via cgo. It parses any valid Postgres SQL and returns a protobuf AST. Do not write a custom parser.

### Supported statements (v1)

- `CREATE TABLE name (col type, ...)`
- `INSERT INTO name (cols) VALUES (...)`
- `SELECT col, ... FROM name WHERE expr`
- `DELETE FROM name WHERE expr` (marks rows dead via xmax, does not remove blocks)

### Supported types (v1)

`TEXT`, `INT`, `BIGINT`, `BOOLEAN`, `FLOAT`

INT, BIGINT, and BOOLEAN values are encoded as heraldic banner pattern bytes; TEXT values are written to sign lines. Type metadata is stored in the table catalog.

---

## 7. Query Planner and Executor

### Planner

v1 planner is trivial — always produces a sequential scan plan. No cost estimation, no index selection.

Plan node types:
- `SeqScanNode { table, filter }`
- `InsertNode { table, values }`
- `CreateTableNode { name, columns }`
- `DeleteNode { table, filter }`

### Executor

Pull-based iterator model (Volcano model). Each node implements:

```go
type Node interface {
    Open(ctx context.Context) error
    Next(ctx context.Context) (Row, error)  // returns io.EOF when done
    Close(ctx context.Context) error
}
```

`SeqScanNode.Next()` asks the storage layer for the next block in the table region, deserializes the row, applies MVCC visibility check, applies the WHERE filter, and returns the row. Repeat until all blocks are exhausted.

---

## 8. Storage Layout

### World coordinate conventions

```
     west  ←  Z=0 (spawn)  →  east
              │
Z < 0         │         Z > 0
WAL lecterns  │         user table data
(transaction  │         (banner+sign strips
 log books)   │          standing on grass)
              │
         underground (Y < 60)
         catalog barrels + control block
```

```
Y       → 64 for table data, < 60 for catalog and control block (underground)
Z axis  → table separation: table N starts at Z = (N-1) × 10000
X axis  → strip layout within a row: each column occupies successive X offsets
```

All user table rows live at Y=64. The catalog and control block live underground at Y=10. Table N (N ≥ 1) occupies Z = (N-1)×10000 through Z = N×10000 - 1 with row R at Z = (N-1)×10000 + R.

Each row is a strip of blocks along X starting at X=0 at a fixed (Y=64, Z):
- Offsets 0–1: xmin (2 banners)
- Offsets 2–3: xmax (2 banners)
- Offset 4+: INT/BIGINT/BOOLEAN columns (1 banner each for INT/BOOL, 2 for BIGINT)
- After all numeric columns: TEXT columns (1 standing sign each)

Strip width = 4 + (1 per INT/BOOLEAN) + (2 per BIGINT) + (1 per TEXT).

Since Minecraft's Z axis is effectively infinite, this allows an unlimited number of tables and rows with no collision.

### Reserved coordinates

| Coordinate | Purpose |
|---|---|
| (0, 10, 0) | Control block — engine metadata, max txid |
| Z < 0, Y=64 | WAL region — lectern log entries |
| Z ≥ 0, Y=10 | Catalog table rows (barrel+JSON) |
| Z ≥ 0, Y=64 | User table rows (banner+sign strips) |

### Free space tracking

Each table maintains an in-memory `RowCount` counter. On INSERT, the row is placed at Z = table.ZStart + table.RowCount and RowCount is incremented. On DELETE (vacuum), ghost rows are replaced with air but Z slots are not reclaimed in v1.

### Table catalog

Table metadata is stored in a reserved catalog area at Y=10, Z=0 onwards. Each row in the catalog describes one user table:

```json
{
  "table_id": 1,
  "name": "players",
  "y_level": 64,
  "columns": [
    {"name": "name", "type": "TEXT", "ordinal": 0},
    {"name": "kills", "type": "INT", "ordinal": 1}
  ]
}
```

Catalog rows use barrel+JSON encoding (not banner+sign) since they are internal metadata.

---

## 9. Row Encoding

Row storage uses a hybrid banner+sign layout. Each row occupies a fixed-width strip of blocks along the X axis at a fixed (Y, Z) coordinate.

### Strip layout

All strips are at Y=64 and extend along X at a fixed Z coordinate. The strip width is determined by the table schema:
```
stripWidth = 4 + sum(1 per INT/BOOLEAN) + sum(2 per BIGINT) + sum(1 per TEXT)
```

- **Banner 0 (X=0)**: xmin bytes 0–5 (first banner of int64)
- **Banner 1 (X=1)**: xmin bytes 6–7 (second banner of int64, zero-padded)
- **Banner 2 (X=2)**: xmax bytes 0–5 — null sentinel is `0xFFFFFFFFFFFFFFFF`
- **Banner 3 (X=3)**: xmax bytes 6–7
- **Banner 4..N**: INT / BIGINT / BOOLEAN columns (6 bytes per banner, packed in ordinal order)
- **Sign 0..M**: TEXT columns (64 chars per sign, OAK_SIGN, 4 lines × 16 chars)

### Banner byte encoding

Each of a banner's 6 pattern layers encodes 1 byte:

- **High nibble (bits 7–4)**: pattern type index 0–15, mapped to these 16 patterns in order:
  `BASE`, `STRIPE_BOTTOM`, `STRIPE_TOP`, `STRIPE_LEFT`, `STRIPE_RIGHT`, `STRIPE_CENTER`,
  `STRIPE_MIDDLE`, `STRIPE_DOWNRIGHT`, `STRIPE_DOWNLEFT`, `SMALL_STRIPES`, `CROSS`,
  `STRAIGHT_CROSS`, `DIAGONAL_LEFT`, `DIAGONAL_RIGHT`, `DIAGONAL_UP_LEFT`, `DIAGONAL_UP_RIGHT`
- **Low nibble (bits 3–0)**: dye color index 0–15, mapped to Minecraft's 16 dye colors:
  `WHITE=0`, `ORANGE=1`, `MAGENTA=2`, `LIGHT_BLUE=3`, `YELLOW=4`, `LIME=5`, `PINK=6`, `GRAY=7`,
  `LIGHT_GRAY=8`, `CYAN=9`, `PURPLE=10`, `BLUE=11`, `BROWN=12`, `GREEN=13`, `RED=14`, `BLACK=15`

Each banner encodes exactly 6 bytes. INT is 4 bytes (1 banner, 2 bytes wasted). BIGINT is 8 bytes (2 banners).

### Sign encoding

- One `OAK_SIGN` per TEXT column
- 4 lines × 16 chars = 64 chars max per sign
- TEXT longer than 64 chars chains multiple signs: sign count = `ceil(len / 64)`

### Null encoding

- **INT / BIGINT null**: all 6 pattern bytes set to `0xFF` in the banner layers
- **TEXT null**: sign line 0 = `"\x00NULL\x00"`, remaining lines empty

### Block type summary

| Data type | Block type |
|---|---|
| xmin, xmax, INT, BIGINT, BOOLEAN | Banner (any color) |
| TEXT | OAK_SIGN |
| WAL entries | Lectern with written book |

---

## 10. Hardware Abstraction Layer (HAL)

### Interface

```go
type Storage interface {
    ReadBlock(ctx context.Context, x, y, z int) ([]byte, error)
    WriteBlock(ctx context.Context, x, y, z int, data []byte) error
    BatchRead(ctx context.Context, positions []BlockPos) ([][]byte, error)
    ForceLoadChunk(ctx context.Context, chunkX, chunkZ int) error
    IsChunkLoaded(ctx context.Context, chunkX, chunkZ int) (bool, error)
}

type BlockPos struct {
    X, Y, Z int
}
```

### Why not RCON

RCON is explicitly rejected for data operations:
- Single-threaded on the Minecraft server side (20 TPS game loop)
- 1446 byte outgoing packet limit
- No batching — each block read is a full round trip
- Known crash bug (MC-72390) under rapid command sequences

RCON must be **disabled** in `server.properties`. All I/O goes through the plugin TCP server.

### Connection management

The HAL client maintains a persistent TCP connection to the plugin. If the connection drops, it retries with exponential backoff (max 30s). The engine does not accept queries until the HAL connection is established.

### Chunk loading

Before any read or write in a new chunk, `ForceLoadChunk` must be called. The HAL maintains a local set of known-forceloaded chunks to avoid redundant calls.

---

## 11. Plugin Binary Protocol

**Port**: 25576
**Transport**: TCP
**Framing**: length-prefixed binary

### Packet format

```
[4 bytes] packet length (uint32, big-endian, excludes length field itself)
[1 byte]  opcode
[N bytes] payload
```

### Opcodes

| Opcode | Name | Direction |
|---|---|---|
| 0x01 | WRITE | client → plugin |
| 0x02 | READ | client → plugin |
| 0x03 | BATCH_READ | client → plugin |
| 0x04 | FORCE_LOAD | client → plugin |
| 0x05 | IS_CHUNK_LOADED | client → plugin |
| 0x06 | BATCH_WRITE | client → plugin |
| 0x10 | ACK | plugin → client |
| 0x11 | DATA | plugin → client |
| 0x12 | BATCH_DATA | plugin → client |
| 0xFF | ERROR | plugin → client |

### WRITE (0x01)

```
[4] x (int32)
[4] y (int32)
[4] z (int32)
[1] block type (uint8): 0x00=barrel, 0x01=banner, 0x02=sign, 0x03=lectern
[4] data length (uint32)
[N] data (UTF-8 string)
```

Block type behavior:
- **0x00 BARREL** — places a barrel, stores data in PersistentDataContainer under key `minesql:minesql_row`
- **0x01 BANNER** — places a standing WHITE_BANNER. Data is 6 hex-encoded banner pattern bytes, each byte = high nibble (pattern type index 0–15) + low nibble (DyeColor ordinal 0–15)
- **0x02 SIGN** — places a standing OAK_SIGN. Data is up to 4 lines delimited by `\0`, each line truncated to 16 chars
- **0x03 LECTERN** — places a LECTERN with a written book. Data is book content, pages separated by `\n---\n`

Empty data (data length 0) sets the block to AIR regardless of block type.

Response: `ACK`

### READ (0x02)

```
[4] x (int32)
[4] y (int32)
[4] z (int32)
```

Response: `DATA`

### BATCH_READ (0x03)

```
[4] count (uint32)
[count × 12] positions (x, y, z as int32 each)
```

Response: `BATCH_DATA`

### FORCE_LOAD (0x04)

```
[4] chunkX (int32)
[4] chunkZ (int32)
```

Response: `ACK`

### ACK (0x10)

Empty payload.

### DATA (0x11)

```
[4] data length (uint32)
[N] data (UTF-8 JSON string, or 0 bytes if block is air/missing)
```

### BATCH_DATA (0x12)

```
[4] count (uint32)
per entry:
  [4] data length (uint32)
  [N] data (UTF-8 JSON string, or 0 bytes if missing)
```

Entries are returned in the same order as the BATCH_READ request positions.

### IS_CHUNK_LOADED (0x05)

```
[4] chunkX (int32)
[4] chunkZ (int32)
```

Response: `DATA` with 1 byte payload — `0x01` if the chunk is loaded, `0x00` if not.

### BATCH_WRITE (0x06)

```
[4] count (uint32)
per entry:
  [4] x (int32)
  [4] y (int32)
  [4] z (int32)
  [1] block type (uint8): 0x00=barrel, 0x01=banner, 0x02=sign, 0x03=lectern
  [4] data length (uint32)
  [N] data (UTF-8 string)
```

Response: `ACK`

### ERROR (0xFF)

```
[4] message length (uint32)
[N] message (UTF-8 string)
```

---

## 12. Write-Ahead Log (WAL)

### Purpose

Guarantee that writes either fully complete or are fully replayed on restart. The database must never be in a partially-written state after a crash.

### WAL region

WAL entries occupy lectern blocks at Z < 0, Y=64.

### Entry format

Each lectern holds a written book. Book pages:

- **Page 1**: `LSN: {n}\nTXID: {n}\nSTATUS: PENDING|COMMITTED`
- **Page 2**: `OP: INSERT|UPDATE_XMAX\nTABLE: {id}`
- **Page 3**: `X: {n}\nY: {n}\nZ: {n}`
- **Page 4**: new value summary (first 200 chars of serialized row, truncated)

Walking up to a lectern and opening it shows the raw transaction log entry in-game.

`status` is either `PENDING` or `COMMITTED`.
`op` is one of `INSERT`, `UPDATE_XMAX` (for delete/update).

The WAL is also mirrored in-memory for performance — the lectern is the durable record, the in-memory map is the fast path. On startup, scan all lecterns in the WAL region to rebuild the in-memory WAL state.

### Write sequence (invariant — never deviate)

```
1. Serialize WAL entry (status=PENDING)
2. WriteBlock(WAL position, entry)
3. Wait for ACK
4. WriteBlock(data position, row data)
5. Wait for ACK
6. Update WAL entry status to COMMITTED
7. WriteBlock(WAL position, updated entry)
8. Wait for ACK
9. Return success to executor
```

If the process dies between steps 2 and 4: on recovery, the PENDING entry is found, the data write is replayed.
If the process dies between steps 4 and 7: on recovery, the PENDING entry is found, the data block is verified to already be correct, entry is marked COMMITTED.
If the process dies before step 2 completes: no entry exists, nothing to recover, the operation is silently rolled back.

### Crash recovery

Recovery runs on every startup before the wire server accepts connections:

```
1. Scan all blocks in the WAL region (Z < 0, Y=64)
2. For each PENDING entry:
   a. Read the target data block
   b. If missing or does not match new_value: replay the data write
   c. Mark entry COMMITTED
3. Accept connections
```

### WAL truncation

Committed WAL entries older than the oldest active transaction can be vacuumed. A background goroutine replaces committed entries with air once they are no longer needed for recovery.

### LSN

Each WAL entry has a monotonically increasing Log Sequence Number (LSN). The current max LSN is stored in the control block. Increment by 1 on each new entry.

---

## 13. MVCC

### Hidden row fields

Every row has two system fields that are never visible to the client:

- `xmin` (int64) — transaction ID that created this row version
- `xmax` (int64 or null) — transaction ID that deleted this row version; null means the row is alive

### Visibility rule

A row is visible to transaction T if:

```
row.xmin <= T.txid  AND  (row.xmax IS NULL OR row.xmax > T.txid)
```

### Operations

**INSERT** by transaction T:
```json
{ "xmin": T.txid, "xmax": null, ... }
```

**DELETE** by transaction T:
- Read the target row
- Write WAL entry (op=UPDATE_XMAX)
- Update the row's xmax to T.txid in the block NBT
- Do NOT remove the block

**UPDATE** by transaction T:
- DELETE the old row (set xmax = T.txid)
- INSERT new row with xmin = T.txid

**SELECT** by transaction T:
- For every row returned by the seq scan, apply the visibility rule
- Discard invisible rows before applying WHERE filters

### Transaction lifecycle

```
BEGIN   → assign new txid from counter, record as active
COMMIT  → remove from active set
ABORT   → for any rows written by this txid, set xmax = txid (mark dead), remove from active set
```

v1 implements autocommit — every statement is its own transaction. Explicit `BEGIN`/`COMMIT` is a v2 feature.

---

## 14. Transaction ID Counter

### Storage

The current maximum transaction ID is stored in the control block at `(0, 10, 0)`: 

```json
{
  "max_txid": 42,
  "max_lsn": 17
}
```

### Startup behavior

On every startup:
1. Read the control block
2. Set the in-memory counter to `max_txid + 100` (safety margin against unwritten increments)
3. Persist the new value back to the control block before accepting connections

### Persistence

The control block is written every N transactions (N=10 for v1). A crash between writes is covered by the +100 startup margin — txids may be skipped but will never collide.

---

## 15. Vacuum

Vacuum is a background goroutine that runs every 60 seconds.

### Algorithm

```
For each table:
  For each chunk in the table region:
    BatchRead all blocks in the chunk
    For each block:
      If row.xmax IS NOT NULL AND row.xmax < oldest_active_txid:
        WriteBlock(position, air)  // setblock air via plugin
        Return slot to free space map
```

`oldest_active_txid` is the minimum txid among all currently active transactions. If no transactions are active, it is the current max_txid.

### Air encoding

Writing air to a block is a WRITE command with an empty data payload (length=0). The plugin interprets this as `world.getBlockAt(x,y,z).setType(Material.AIR)`.

---

## 16. Docker Setup

### docker-compose.yml (full stack)

Two services:
- `minecraft` — Paper server with minesql-plugin, port 25576 exposed internally
- `minesql` — Go engine, port 5432 exposed to host

The `minesql` service depends on `minecraft` being healthy. Health check: attempt TCP connection to port 25576.

### docker-compose.dev.yml (dev mode)

One service:
- `minecraft` — Paper server with plugin, port 25576 exposed to host (127.0.0.1:25576)

Go engine runs locally and connects to `localhost:25576`.

### Dockerfile (minecraft image)

Base: `eclipse-temurin:21-jre`

Steps:
1. Download Paper jar (pinned version)
2. Copy minesql-plugin jar
3. Copy pre-seeded world from `docker/world/`
4. Set `eula=true`, disable RCON (`enable-rcon=false`), enable online-mode=false
5. Expose ports 25565 (game), 25576 (plugin TCP)

### Pre-seeded world

The `docker/world/` directory contains a minimal Minecraft world where:
- The control block at (0, 10, 0) exists with `max_txid=0, max_lsn=0`
- The catalog table region (Y=10) is forceloaded
- The WAL region (Z < 0, Y=64) is forceloaded

This means the engine can start and accept connections immediately without a bootstrap step.

---

## 17. v1 Scope

v1 is complete when all of the following work correctly and reliably:

- `psql -h localhost -p 5432` connects successfully
- `CREATE TABLE name (col TYPE, ...)` creates a table entry in the catalog
- `INSERT INTO name VALUES (...)` writes a banner+sign strip to the world
- `SELECT * FROM name` returns all visible rows via seq scan
- `SELECT * FROM name WHERE col = val` applies a WHERE filter
- `DELETE FROM name WHERE col = val` marks rows dead via xmax
- Data survives engine restart (WAL recovery + persistent blocks)
- Two concurrent read transactions do not block each other
- Docker Compose brings up the full stack with one command

v1 does NOT include: joins, indexes, explicit transactions, prepared statements, authentication, TLS, TOAST, vacuum (background goroutine exists but is optional for v1 completion).

---

## 18. v2 Scope

Post-v1, in rough priority order:

- **Vacuum** — background dead row cleanup, slots returned to free space map
- **Explicit transactions** — `BEGIN` / `COMMIT` / `ROLLBACK`
- **B-tree index** — physically built as a tree of blocks in the world, traversable on foot
- **JOIN support** — nested loop join as a starting point
- **TOAST** — large values split across multiple blocks, reassembled on read
- **Authentication** — basic username/password in startup message
- **Web dashboard** — live query stats, active transactions, buffer pool visualization

---

## 19. Minecraft Chat Interface

### Overview

The Minecraft Chat Interface allows players to execute SQL queries by typing `/sql <query>` in the Minecraft chat. The plugin captures this command, opens a connection to the engine's query endpoint, executes the SQL, and returns formatted results to the player.

### Architecture

```
Player types /sql SELECT * FROM players
    → Plugin captures command
    → Plugin opens TCP connection to engine on port 5456
    → Engine parses SQL, executes, returns results
    → Plugin formats results as colored chat components
    → Plugin sends formatted response to player
```

### Protocol

**Port**: 5456
**Direction**: plugin → engine (opposite of the HAL binary protocol on 25576)
**Transport**: TCP, line-delimited text

1. Plugin connects to `localhost:5456`
2. Plugin sends the SQL query as a single line (UTF-8, `\n` terminated)
3. Engine executes the query
4. Engine responds with results as JSON:

```json
{
  "columns": ["name", "kills"],
  "rows": [
    ["swapnil", "42"],
    ["steve", "7"]
  ],
  "row_count": 2,
  "truncated": false
}
```

5. If an error occurs, the engine responds with:

```json
{
  "error": "syntax error at or near \"SELEC\""
}
```

6. Plugin closes the connection after receiving the response.

### Result Rendering

The plugin formats results as colored Minecraft chat components:

- **Header row**: column names in gold (`§6`), separated by ` | `
- **Separator line**: dashes in gray (`§7`)
- **Data rows**: alternating green (`§a`) and white (`§f`) per row
- **Truncation message**: if more than 8 rows, show first 8 rows then "§7... N more rows"
- **Row count footer**: gray text showing row count
- **Error messages**: displayed in red (`§c`)

Example output in chat:

```
§6name           | kills
§7-----------------------------
§asteve          | 7
§fswapnil        | 42
§aherobrine      | 999

§72 rows returned
```

### v1 Limitations

- Single query per `/sql` command (no multi-statement)
- Results limited to 8 rows displayed in chat
- No query history or tab completion
- No result pagination
