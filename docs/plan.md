# Implementation plan

Working plan for the first version of Loci. CLAUDE.md holds the design rules. This file holds the order of work and the decisions that only matter while building. Tick milestones off as they land, and delete this file once they are all done.

## Tool surface

Each tool below is one `internal/service` method, one MCP tool, and one cobra command (`entity_search` is `loci entity search`).

| Area   | Tool            | Notes |
|--------|-----------------|-------|
| Entity | `entity_search` | Trigram + BM25 over name, aliases, description. Ends with the `(new)` placeholder row. |
|        | `entity_create` | Requires the entity placeholder uuid. |
|        | `entity_get`    | The entity, its edges (neighbor name and type, direction-resolved edge name), and the titles of its linked memories grouped by link type. |
|        | `entity_update` | Name, aliases, description, type. Re-checks uniqueness against the (possibly new) type. |
|        | `entity_delete` | Removes its edges and links. Refused, listing the memories, if any memory links only to it. |
| Edge   | `edge_create`   | By triple. Idempotent. |
|        | `edge_delete`   | By triple. |
| Memory | `memory_search` | BM25 over title and content. Ends with the `(new)` placeholder row. |
|        | `memory_create` | Requires the memory placeholder uuid and at least one link. |
|        | `memory_get`    | The memory with its links. |
|        | `memory_update` | Title and content. |
|        | `memory_delete` | Removes its links. |
|        | `memory_list`   | Full memories linked to an entity, optionally filtered by link type. |
| Link   | `link_create`   | By triple. Idempotent. |
|        | `link_delete`   | By triple. Refused if it is the memory's last link. |

There is no `edge_list`: `entity_get` already returns every edge, so a separate tool would only duplicate it. `memory_list` stays because `entity_get` returns titles only.

## Storage

bbolt buckets:

- `entities`, `memories`: uuid → JSON record.
- `edges`: `from|type|to` → empty. `edges_by_to`: `to|type|from` → empty, for reading from the other end.
- `links`: `memory|type|entity` → empty. `links_by_entity`: `entity|type|memory` → empty.
- `meta`: placeholder uuids per kind, schema version.

Trigram and BM25 indexes are in memory, rebuilt from the buckets on open (as engRam does for BM25).

Every store operation is one bbolt transaction. Invariants that span records are checked inside that transaction: a memory's last link, deleting an entity that some memory links to alone, name/alias uniqueness, and consuming the placeholder together with the create.

## Defaults chosen

- Case folding: `cases.Fold()` from `golang.org/x/text`, in one function used by search and uniqueness checks.
- Symmetric edges are stored with `from` < `to` by uuid, so both orderings resolve to one key.
- Entity ranking fuses the trigram and BM25 rankings with reciprocal rank fusion (k = 60), as engRam fuses its legs.

## Milestones

Each ends with `task test` passing.

- [x] **1. Scaffold.** `go.mod`, `Taskfile.yml` (`build`, `test`), cobra root, `loci version` with ldflags injection, viper config (XDG paths, `LOCI_*` overrides, `--config`, never writes the file).
- [ ] **2. Domain and store.** Closed type sets with inverse names, name normalization and validation, the case-fold function, bbolt schema and CRUD with the invariants above.
- [ ] **3. Search.** Trigram and BM25 indexes, fused entity ranking, memory BM25.
- [ ] **4. Service.** Every tool in the table, including placeholder rotation on every search and create. Most tests live here.
- [ ] **5. MCP.** Tool adapters over the service, tested with mcp-go's in-process client.
- [ ] **6. Daemon.** Port engRam's spawn lock, ownership lock, version preamble, idle shutdown, and `loci serve` (the stdio ↔ socket pipe).
- [ ] **7. CLI.** Cobra commands that call tools through an mcp-go client over the socket. Human-readable tables by default, `--json` prints the structured result. Parity test comparing registered tools with cobra commands, with an explicit allowlist for commands that are not tools (`version`, `serve`, `daemon`). End-to-end integration test through a real daemon.
