# mineSQL

> A relational database engine that uses a Minecraft world as its physical storage backend.

Connect with `psql`. Run real SQL. Watch your data persist as blocks in the ground.

```
$ psql -h localhost -p 5432 -U minecraft -d minesql
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

Somewhere in the Minecraft world, a barrel block at coordinates `(32, 64, 15)` now holds:

```json
{"xmin": 1, "xmax": null, "name": "swapnil", "kills": 42}
```

---

## What It Is

mineSQL is a Postgres-wire-compatible relational database engine written in Go. It implements real database internals — WAL, MVCC, a query executor, and a SQL parser — but instead of writing to disk, it stores every row as NBT data inside Minecraft barrel blocks in a live Minecraft world.

Any tool that speaks Postgres works out of the box. `psql`, database drivers in any language, ORMs — none of them know they are talking to a Minecraft world.

---

## How Storage Works

The Minecraft world is divided into regions. Each table occupies a fixed Y level — table 0 lives at Y=64, table 1 at Y=128, and so on. Within a Y level, chunks (16×16 block columns) act as heap pages. Each block position within a chunk holds one row, stored as JSON in the barrel's NBT tag.

A sequential scan flies through every chunk at a table's Y level, reads the NBT from each barrel, deserializes the row, applies filters, and streams results back to the client. A write places a new barrel at the next available block position and flushes a WAL entry before the data block is touched.

Dead rows from deletes and updates are never immediately removed. A background vacuum goroutine periodically scans for rows that no active transaction can see and replaces them with air.

---

## Architecture

```
psql / any Postgres client
        │  Postgres wire protocol (port 5432)
        ▼
┌─────────────────────────────────┐
│  Wire Protocol → SQL Parser     │
│  Query Planner → Executor       │
│  Storage Layer → WAL → MVCC     │
│  HAL (Go)                       │
└─────────────────────────────────┘
        │  custom binary TCP protocol (port 25576)
        ▼
┌─────────────────────────────────┐
│  minesql-plugin (Paper/Java)    │
│  thin I/O bridge — READ/WRITE   │
└─────────────────────────────────┘
        │
        ▼
  Minecraft World
```

The Paper plugin is a separate repository: [swapnil404/minesql-plugin](https://github.com/swapnil404/minesql-plugin)

---

## Getting Started

### Prerequisites

- Go 1.22+
- Docker + Docker Compose
- `psql`

### Run

```bash
git clone https://github.com/swapnil404/minesql
cd minesql
docker compose up
```

Wait for Minecraft to finish loading (~30s), then connect:

```bash
psql -h localhost -p 5432 -U minecraft -d minesql
```

### Dev Mode

```bash
# Minecraft in Docker, engine runs locally
docker compose -f docker-compose.dev.yml up
go run ./cmd/minesql
```

---

## Inspiration

Inspired by [discodb](https://github.com/lasect/discodb) — a database that uses Discord as its storage backend. mineSQL applies the same philosophy to Minecraft, with the added dimension that the world becomes a live, walkable visualization of database internals.

---

## License

MIT
