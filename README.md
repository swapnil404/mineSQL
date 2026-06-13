# mineSQL

> A relational database engine that uses a Minecraft world as its physical storage backend.

Connect with `psql` or chat `/sql` in-game. Run real SQL. Watch your data persist as blocks in the ground.

```
# From any Postgres client:
$ psql -h localhost -p 5433 -U minecraft -d minesql
psql (15.0)
Type "help" for help.

minesql=# CREATE TABLE players (name TEXT, kills INT);
CREATE TABLE

minesql=# INSERT INTO players VALUES ('swapnil', 42);
INSERT 0 1

minesql=# SELECT * FROM players WHERE kills > 10;
  name   | kills
---------+-------
 swapnil |    42
(1 row)
```

```
# Or from inside Minecraft chat:
/sql SELECT * FROM players WHERE kills > 10

  name           | kills
-----------------------------
  swapnil        | 42

2 rows returned
```

---

## Getting Started

### Prerequisites

- Go 1.22+
- Docker + Docker Compose
- `psql` (or any Postgres client)

### Run

```bash
git clone https://github.com/swapnil404/minesql
cd minesql
docker compose up
```

Wait for Minecraft to finish loading (~30s), then connect:

```bash
# Via psql (any Postgres client)
psql -h localhost -p 5433 -U minecraft -d minesql
```

```bash
# Or via in-game chat (join the Minecraft server on localhost:25565)
# Type in chat:
/sql SELECT * FROM players
```

### Dev Mode

```bash
# Minecraft in Docker, engine runs locally
docker compose -f docker-compose.dev.yml up
go run ./cmd/minesql
```

---

## What It Is

mineSQL is a Postgres-wire-compatible relational database engine written in Go. It implements real database internals — WAL, MVCC, a query executor, and a SQL parser — but instead of writing to disk, it stores every row as a strip of banner blocks and signs standing on grass in a live Minecraft world.

Any tool that speaks Postgres works out of the box. `psql`, database drivers in any language, ORMs — none of them know they are talking to a Minecraft world.

---

## How Storage Works

The world is the database. Spawn is the origin.

**Walk east** — you are walking through your tables. Each table occupies a Z region on the surface starting at Z=0. Every row is a horizontal strip of standing banners and signs. Banner blocks encode INT, BIGINT, and BOOLEAN columns as heraldic pattern layers (6 bytes per banner, pattern type = high nibble, dye color = low nibble). Signs hold TEXT columns (64 chars per sign). Each row strip stands on the grass at Y=64, visible from above.

**Walk west** — you are walking through the transaction log. Each WAL entry is a lectern block holding a written book. Open any lectern and read the raw transaction: LSN, TXID, STATUS, operation, target coordinates, new value. The further west you go, the older the transaction.

**Dig down** — you are reading internal metadata. The catalog (table definitions) and control block (max transaction ID) live underground at Y=10, out of sight.

A sequential scan flies through the Z region of a table, reads the banner patterns and sign text from each row strip, deserializes the row, applies MVCC visibility and WHERE filters, and streams results back to the client. A write appends a new strip at the next Z position and flushes a WAL lectern entry before the data blocks are touched.

Dead rows from deletes and updates are never removed — their xmax banner is updated to mark them invisible to new transactions.

---

## Architecture

```
psql / any Postgres client         Minecraft chat (/sql)
         │  port 5433                    │  port 5456
         ▼                               ▼
┌─────────────────────────────────────────────┐
│  Wire Protocol         Chat Server          │
│  (pgproto3)            (JSON line-delimited)│
├─────────────────────────────────────────────┤
│  SQL Parser (pg_query)                      │
├─────────────────────────────────────────────┤
│  Executor (planning, execution, MVCC)       │
├─────────────────────────────────────────────┤
│  Storage (banner+sign encoding, catalog)    │
├─────────────────────────────────────────────┤
│  WAL (write-ahead log, crash recovery)      │
├─────────────────────────────────────────────┤
│  HAL — TCP client to Paper plugin           │
└─────────────────────────────────────────────┘
         │  custom binary TCP (port 25576)
         ▼
┌─────────────────────────────────┐
│  minesql-plugin (Paper/Java)    │
│  thin I/O bridge — READ/WRITE   │
└─────────────────────────────────┘
         │
         ▼
  Minecraft World (superflat, structures off)
```

The Paper plugin is a separate repository: [swapnil404/minesql-hal](https://github.com/swapnil404/minesql-hal)

---

## World Layout

```
     west  ←  Z=0 (spawn)  →  east
              │
Z < 0         │         Z > 0
WAL lecterns  │         user table data
(transaction  │         (banner+sign strips
 log books)   │          standing on grass)
              │
         underground (Y=10)
         catalog + control block
```

## License

MIT
