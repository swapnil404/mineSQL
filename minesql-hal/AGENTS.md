# AGENTS.md

This file helps coding agents (Claude, Cursor, Codex, etc.) understand the mineSQL-HAL codebase and work effectively within it.

---

## What This Is

mineSQL-HAL is a Paper plugin that acts as the hardware abstraction layer for [mineSQL](https://github.com/swapnil404/minesql). It runs a custom binary TCP server inside a live Minecraft server, exposing block read and write operations to the mineSQL engine over a lightweight binary protocol.

This plugin has no standalone purpose. It exists entirely to serve the mineSQL engine. All database logic lives in the Go engine. This plugin is a dumb I/O bridge — it receives commands, touches blocks, and sends responses. Nothing more.

---

## Project Structure

```
minesql-hal/
├── pom.xml
└── src/
    └── main/
        ├── java/dev/swapnil404/minesqlhal/
        │   ├── Main.java            # plugin entry point, onEnable/onDisable
        │   ├── TCPServer.java       # accepts connections, spawns handler threads
        │   ├── BlockHandler.java    # READ/WRITE/BATCH_READ/FORCE_LOAD/IS_CHUNK_LOADED/BATCH_WRITE logic
        │   └── QueryHandler.java    # /sql command handler, chat interface to mineSQL engine
        └── resources/
            └── plugin.yml
```

---

## Build & Install

```bash
# build
mvn package

# output jar
target/minesql-hal-0.1.0.jar

# install — copy jar to Paper server plugins folder and restart
cp target/minesql-hal-0.1.0.jar /path/to/server/plugins/
```

---

## Protocol

The plugin listens on **port 25576** over TCP. All packets are length-prefixed binary:

```
[4 bytes] packet length (uint32, big-endian, excludes length field)
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
[4] x (int32) [4] y (int32) [4] z (int32)
[1] block type (uint8): 0x00=barrel, 0x01=banner, 0x02=sign, 0x03=lectern
[4] data length (uint32)
[N] data (UTF-8 string)
```
Places a block at (x, y, z) of the specified type and stores data in PersistentDataContainer under key `minesql:minesql_row`. Responds with ACK.

Block type behavior:
- **0x00 BARREL** — existing behavior. Places a barrel, stores data as-is in PDC.
- **0x01 BANNER** — places a standing WHITE_BANNER. Data must be exactly 12 hex characters (6 bytes). Each byte encodes one pattern layer: high nibble = pattern type index (0–15 mapped to 16 PatternType values), low nibble = DyeColor ordinal (0–15). Also stores the hex string in PDC.
- **0x02 SIGN** — places a standing OAK_SIGN. Data is a UTF-8 string with up to 4 lines delimited by `\0` (null byte). Each line is truncated to 16 chars. Also stores full string in PDC.
- **0x03 LECTERN** — places a LECTERN. Data is the book content with pages separated by `\n---\n`. Also stores full string in PDC.

Empty data (data length 0) sets the block to AIR regardless of block type.

### READ (0x02)
```
[4] x (int32) [4] y (int32) [4] z (int32)
```
Reads `minesql_row` from block at (x, y, z). Responds with DATA. If block is air or has no data, responds with DATA of length 0.

### BATCH_READ (0x03)
```
[4] count (uint32)
[count × 12] positions (x, y, z as int32 each)
```
Reads all blocks in a single server thread invocation. Responds with BATCH_DATA in the same order as the request positions.

### FORCE_LOAD (0x04)
```
[4] chunkX (int32) [4] chunkZ (int32)
```
Force loads the chunk so it stays in memory regardless of player proximity. Responds with ACK.

### IS_CHUNK_LOADED (0x05)
```
[4] chunkX (int32) [4] chunkZ (int32)
```
Checks if the chunk at (chunkX, chunkZ) is currently loaded in memory. Responds with DATA containing 1 byte: `0x01` if loaded, `0x00` if not.

### BATCH_WRITE (0x06)
```
[4] count (uint32)
per entry: [4] x (int32) [4] y (int32) [4] z (int32) [1] block type (uint8) [4] data length (uint32) [N] data (UTF-8 string)
```
Writes count blocks in a single server thread invocation. Responds with ACK. Works identically to WRITE repeated count times — empty data means set block to air.

### ACK (0x10)
Empty payload.

### DATA (0x11)
```
[4] data length (uint32)
[N] data (UTF-8 string, or 0 bytes if missing)
```

### BATCH_DATA (0x12)
```
[4] count (uint32)
per entry: [4] data length (uint32) [N] data (UTF-8 string, or 0 bytes if missing)
```

### ERROR (0xFF)
```
[4] message length (uint32)
[N] message (UTF-8 string)
```

---

## Minecraft Chat Interface

The plugin provides a `/sql` chat command that lets ops execute queries from in-game chat via a short-lived TCP connection to the mineSQL engine.

### Command

- `/sql <query>` — registered in `plugin.yml`
- Permission: `minesqlhal.sql` (default: `op`)
- `QueryHandler.java` implements `CommandExecutor` and handles the command

### Protocol (port 5456)

Port 5456 is separate from the HAL protocol port (25576). Each `/sql` invocation opens a short-lived TCP connection to `localhost:5456`.

**Request** (client → engine):
```
[4 bytes] payload length (uint32, big-endian)
[N bytes] UTF-8 query string
```

**Response** (engine → client):
```
[1 byte]  status (0x00 = success, 0xFF = error)
[4 bytes] payload length (uint32, big-endian)
[N bytes] UTF-8 JSON
```

### Result JSON format

```json
{"columns": ["name", "kills"], "rows": [["swapnil", 42]]}
```

### Chat rendering

Results are rendered in chat:
- **Gold** (`ChatColor.GOLD`) — column headers
- **Gray** (`ChatColor.GRAY`) — separator line
- Alternating **white** / **gray** rows (`ChatColor.WHITE` / `ChatColor.GRAY`)
- Maximum **8 rows** displayed; excess rows are noted with a "… and N more rows" message
- Errors rendered in **red** (`ChatColor.RED`)

### Threading

The TCP connection to the engine runs on an async scheduler thread (`runTaskAsynchronously`). Chat rendering is dispatched back to the main server thread via `runTask`.

---

## Storage Format

Row storage uses a hybrid banner+sign layout. Each row occupies a fixed-width strip of blocks along the X axis at a fixed (Y, Z) coordinate.

Strip layout:
- Banners 0-1: xmin (int64, 8 bytes)
- Banners 2-3: xmax (int64, null=0xFFFFFFFFFFFFFFFF)
- Banners 4..N: INT/BIGINT/BOOLEAN columns (6 bytes per banner, columns packed in ordinal order)
- Sign 0..M: TEXT columns (64 chars per sign, one sign per TEXT column)

Banner byte encoding:
- Each of the 6 pattern layers encodes 1 byte
- High nibble (bits 7-4): pattern type index (0-15, mapped to 16 chosen pattern types)
- Low nibble (bits 3-0): dye color index (0-15, maps directly to Minecraft's 16 dye colors)
- The encoding table is fixed and defined in the Go codec package

WAL storage:
- Each WAL entry is a lectern block at a fixed coordinate in the WAL region (Z < 0, Y=64)
- The lectern holds a written book with pages:
  - Page 1: LSN + TXID + STATUS (PENDING or COMMITTED)
  - Page 2: Operation + Table ID
  - Page 3: Target coordinates (X Y Z)
  - Page 4: New value summary (truncated to book page limit)
- Walking up to a lectern and opening it shows the raw transaction log entry in-game

---

## Critical Rules

**Threading** — Minecraft is not thread safe. Every block read or write must be scheduled on the main server thread using Paper's scheduler. The TCP server runs on its own threads. Never touch the world directly from a TCP handler thread — always schedule via `Bukkit.getScheduler().runTask(plugin, runnable)`.

**Air encoding** — A WRITE with data length 0 means set the block to air (`Material.AIR`). This is how the mineSQL vacuum process removes dead rows.

**Block types** — Row storage uses a hybrid banner+sign model. INT/BIGINT/BOOLEAN columns are encoded as bytes into banner pattern layers (`Material.WHITE_BANNER` or any color banner). TEXT columns are stored in signs (`Material.OAK_SIGN`). System fields (xmin, xmax) occupy the first four banners (0-1 for xmin, 2-3 for xmax) in every row strip. WAL entries are stored as written books inside lectern blocks (`Material.LECTERN`). Never use barrels for row or WAL storage.

**NBT encoding** — Banner rows: pattern layers encode 1 byte each (high nibble = pattern type index 0-15, low nibble = dye color index 0-15). Each banner holds 6 bytes. Signs: each of the 4 lines holds up to 16 UTF-8 characters of TEXT column data (64 chars per sign total). Lectern WAL: written book pages store LSN, TXID, STATUS, operation, table ID, target coordinates, and new value summary as human-readable text.

**Block state ordering** — Always call `block.setType()` before `block.getState()`. Never call `block.setBlockData()` between `setType()` and `getState()` — it invalidates the state snapshot, causing `ClassCastException` (e.g. `CraftBlockState` cannot be cast to `Banner`). The correct order is: `setType` → `getState` (cast immediately) → `setBlockData` (for directional blocks) → modify state → `update(true)`.

**No business logic** — This plugin does not know what a row is, what a transaction is, or what mineSQL is doing. It receives coordinates and bytes, reads or writes blocks, and responds. Any logic beyond that belongs in the Go engine.

**RCON** — RCON must be disabled in `server.properties`. This plugin is the only external I/O interface to the Minecraft server.

---

## Dependencies

- Paper API 1.21 (`io.papermc.paper:paper-api:1.21-R0.1-SNAPSHOT`, scope: provided)
- Java 21
- Maven (build only)

No external runtime dependencies. The Paper API is provided by the server at runtime.

---

## Testing

There is no unit test suite for this plugin — block I/O requires a live Minecraft server. Test manually by:

1. Building the jar with `mvn package`
2. Dropping it into a running Paper server's plugins folder
3. Running the Go test client from the mineSQL repo: `go test ./internal/hal/integration_test.go`

Set `MINESQL_MINECRAFT_ADDR=localhost:25576` before running integration tests.

Verify each opcode manually in order: WRITE → READ (round trip) → BATCH_READ → BATCH_WRITE → FORCE_LOAD → IS_CHUNK_LOADED.
