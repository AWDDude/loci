# Loci

Entity-oriented persistent memory MCP server for coding agents. Single statically-linked Go binary. Experimental successor to engRam (`~/projects/AWDDude/engRam`), whose daemon and config handling are the reference implementation for the same concerns here.

**Status: experimental, released (v0.1.0 onward).** README.md describes the design, and this file holds the rules the code must follow and the decisions behind them.

## Invariants

Breaking any of these is a bug, not a trade-off.

1. **Every memory links to at least one entity.** Creating a memory with no links is rejected, and so is removing a memory's last link.
2. **Creating an entity or memory requires the current placeholder uuid** for its kind. See [Search before create](#search-before-create).
3. **MCP and CLI have exact parity.** Every capability is implemented once in `internal/service` and exposed through both an MCP tool and a cobra command. Adding one without the other is a bug, and a test enumerates both surfaces and fails on any mismatch.
4. **Entity names and aliases are unique per type, case-insensitively.** Checked on create and update against every name and alias of entities of the same type.
5. **Types are closed sets defined in code.** Entity types, link types, and edge types are never free-form. An unknown type is rejected with an error listing the valid ones.

## Data model

- **Entity:** uuid, name, aliases, description (one line), type, timestamps.
  Types: `person`, `organization`, `project`, `repository`, `service`, `tool`, `place`, `concept`.
- **Memory:** uuid, title, content (free text), timestamps. No tags.
- **Link** (memory → entity): memory uuid, link type, entity uuid. Identified by that triple.
  Types: `attribute`, `preference`, `event`, `decision`, `mention`.
- **Edge** (entity → entity): from, edge type, to. Identified by that triple. Directional and stored once, and a symmetric type is stored with the smaller uuid as `from` so `A spouse_of B` and `B spouse_of A` are the same edge. Queries from the `to` side report the inverse name. An edge from an entity to itself is rejected.
  Types and inverses: `parent_of`/`child_of`, `spouse_of` (symmetric), `sibling_of` (symmetric), `member_of`/`has_member`, `owns`/`owned_by`, `works_on`/`worked_on_by`, `part_of`/`has_part`, `depends_on`/`depended_on_by`, `uses`/`used_by`, `related_to` (symmetric).

Entities and memories get a Loci-generated uuid, returned in every result. Links and edges have no uuid of their own: their identity is exact, so a uuid would only be a second name for the triple. For the same reason they need no search-before-create placeholder; a duplicate is detected exactly, and the fuzzy part (which entity) was already resolved by the search that produced the entity uuid. There is no edit history.

### What can change

- Entity name, aliases, description, and type: explicit edits only. Renaming does **not** add the old name as an alias. If the old name still matters, the caller adds the alias itself (an automatic alias would keep typo fixes like "Davd" around forever).
- Memory title and content: editable. Links: add and remove only, never retyped.
- Links and edges: create and delete only, both addressed by their triple. Creating one that already exists is a no-op that returns the existing record.
- `edge_create` and `edge_delete` also accept inverse names (`B child_of A` is stored as `A parent_of B`), so a caller can reuse the name `entity_get` showed it, from either end.
- Updates are partial: an omitted field is unchanged. `aliases`, when given, replaces the whole list.

### Deletes

- Memory: removes its links.
- Edge or link: removes just that record.
- Entity: removes its edges and the links pointing at it. **Refused**, listing the memories, if any memory links only to that entity (deleting it would violate invariant 1).

## Search before create

Every search result ends with a "new" placeholder row for that kind. `entity_create` and `memory_create` require the placeholder's uuid. The placeholder uuid is regenerated on **every search of that kind and every create**, so a create only succeeds after the latest search. A stale uuid returns a short "search again" error. The create does not run a search itself or return candidates.

- Placeholders live in a metadata bucket, not as records, so they never appear in lists or counts.
- A search result carries the placeholder in a `placeholder` field beside the hits; the CLI renders it as the trailing `(new)` row.
- A create rejected for any other reason (a taken name, a missing entity) leaves the placeholder unchanged, so the caller can fix the input and retry without searching again. The check and rotation run in the create's transaction, which rolls both back.
- One placeholder per kind is global across all sessions. A search in session B invalidates session A's placeholder, so A has to search again. This is **accepted**: Loci serves one user on one workstation, and the retry is rare. Do not "fix" it with per-session or per-query tokens without revisiting the design.
- This is the pattern for anything that must not be duplicated.

Rejected alternatives: separate search tokens (the user dislikes passing tokens around); a stateful resolve → choose conversation with pending selections per session (it replaced this, being stateless and working across CLI invocations); `create` that searches and returns candidates (makes create double as a search tool).

## Names and matching

- Names and aliases may contain whitespace. On write, trim and collapse runs of whitespace to one space. Reject control characters and newlines.
- Casing is stored as given (`engRam`, `jq`) and never normalized.
- One case-folding function, `model.Fold` (full Unicode folding via `golang.org/x/text/cases`, so "Straße" matches "STRASSE"), defines "same name". Search and uniqueness checks both call it. **Do not store a normalized key** next to the display name: the key is derived, so storing it only creates a way for the two to drift.

## Search and retrieval

- **No embeddings.** The project's hypothesis is that the agent supplies the semantics and Loci supplies the structure: find an entity, walk its edges, list linked memories filtered by link type.
- Entity search: trigram similarity over names and aliases, plus BM25 over name, aliases (weighted 2x), and description, fused by reciprocal rank fusion (k = 60) as engRam fuses its legs. Optional type filter.
- Trigram matching compares the query with each whole name and alias and with each of their words, taking the best (like pg_trgm's `word_similarity`). Whole-name Jaccard alone lets "davd" miss "David Kittle". Threshold 0.3.
- Memory search: BM25 over title (weighted 2x) and content, as a fallback when the agent doesn't know which entity to start from.
- `limit` defaults to 20 when omitted or 0. There is no "unlimited" value and no config key, until one is needed.
- An ambiguous query ("david") returns every candidate; ties break by uuid. The caller picks, so tests must not assert an order between equally good matches.
- There are no memory-to-memory links and no tags. Both compete with entities as a way to organize, and the experiment needs one. If stale memories become a problem, the planned addition is a single `supersedes` relation.

## Tools

Each tool is one `internal/service` method, one MCP tool, and one cobra command (`entity_search` is `loci entity search`).

| Area   | Tools |
|--------|-------|
| Entity | `entity_search`, `entity_create`, `entity_get`, `entity_update`, `entity_delete` |
| Edge   | `edge_create`, `edge_delete` |
| Memory | `memory_search`, `memory_create`, `memory_get`, `memory_update`, `memory_delete`, `memory_list` |
| Link   | `link_create`, `link_delete` |

- `entity_get` returns the entity's neighborhood: its edges (the other entity's name, type and uuid, with the edge named from this end) and its memory titles grouped by link type. So there is no `edge_list`. `memory_list` exists because `entity_get` returns titles only.
- Service input structs carry the JSON tags that are the MCP argument names, and the MCP adapter binds straight into them, so the wire names live in one place.
- Tool enums (entity, link and edge types, including inverse edge names) are generated from `model`, and a test checks them.
- Results are JSON objects, never bare arrays, since MCP structured content must be an object (`memory_list` returns `{"memories": [...]}`). Empty lists serialize as `[]`. Delete tools return `{"deleted": <id or triple>}`.
- A service error is returned as an MCP tool error (`isError: true`) carrying the service's message, never a protocol fault.

## Storage

- Buckets: `entities` and `memories` (uuid → JSON); `edges` (`from/type/to`) and `edges_by_to` (`to/type/from`); `links` (`memory/type/entity`) and `links_by_entity` (`entity/type/memory`); `meta` (schema version, placeholders). Edges and links are pure keys, written once per end so either end is a prefix scan.
- Every store operation is one bbolt transaction, and the invariants that span records are checked inside it. The store checks relationships; formatting and validation happen in the service.
- The trigram and BM25 indexes are in memory, rebuilt from the buckets on open, and updated under the same lock as the transaction that changes them, so an index cannot drift from the records.

## Stack

- **Storage:** bbolt, one file.
- **MCP:** mcp-go over stdio, proxied to a shared daemon.
- **CLI:** cobra. **Config:** viper.
- **Daemon:** ported from engRam. The first client to need the database spawns a daemon that owns it, and every MCP session and every CLI command connects over a unix socket. The CLI never opens the bbolt file directly, since bbolt locks it to one process. `loci serve` is the stdio end an MCP client runs.
  - Kept from engRam: spawn lock, ownership lock, version preamble, idle shutdown after 10 minutes, retirement of a daemon running another build.
  - No model to load, so the daemon opens the store synchronously and the spawn timeout is 15s.
  - A spawned daemon gets the client's `--config` as an explicit absolute-path flag. engRam passed its config path in an environment variable, which a flag does not travel through on its own.
  - A client that hangs up before reading the preamble (`loci daemon status`, liveness probes) is an ordinary session end, not logged as a failure.
- **The socket speaks only MCP.** MCP sessions are a byte pipe between stdio and the socket. Each CLI command is an MCP client (mcp-go `transport.NewIO` over the socket) that makes one `tools/call` and formats the result, and `--json` prints the tool's structured result as is. Rejected: a second RPC protocol for the CLI, which would be a second wire format to keep in sync with the tools.

## CLI

- Human-readable output by default. Every command accepts `--json`.
- Required uuids are positional and everything else is a flag. `memory create` takes repeatable `--link TYPE:ENTITY_UUID`, and `--content -` reads stdin. Update commands send only the flags given; `--aliases ""` clears the list.
- Each tool command carries a `loci/tool` annotation. The parity test walks the command tree and checks those against the tools the server registers; commands that are not tools (`version`, `serve`, `daemon` and its subcommands) are on an explicit allowlist.
- Each CLI invocation is its own client, so a placeholder uuid from one `loci ... search` stays valid for a later `loci ... create` unless another search or create intervened.

## Config

- `~/.config/loci/config.yaml` (honors `XDG_CONFIG_HOME`). YAML is the only documented format.
- Any key can be overridden with a `LOCI_*` environment variable (e.g. `LOCI_DB_PATH`), and the file path with `--config`.
- A missing config means defaults. **Never write the config file** (it may be dotfiles-managed).

## Data layout

```
~/.local/share/loci/     # $XDG_DATA_HOME/loci if set
├── loci.bbolt           # .bbolt so nothing mistakes it for SQLite
├── loci.sock            # daemon socket (0600)
├── daemon.lock          # held by the daemon for its whole life
├── spawn.lock           # held by a client while it starts a daemon
├── daemon.pid
└── daemon.log
```

The daemon's runtime files sit beside the database, so a config with a different data path gets its own daemon.

## Layout

```
cmd/loci/           # entry point, cobra root
internal/config/    # viper loading and defaults
internal/daemon/    # socket, spawn, serve loop
internal/model/     # records, closed type sets, name normalization and Fold
internal/store/     # bbolt schema, trigram and BM25 indexes
internal/service/   # every capability, implemented once
internal/mcp/       # thin MCP tool adapters over service
internal/cli/       # cobra commands, each an MCP client of the daemon
```

## Build and release

- **Task is the only build entry point** (`Taskfile.yml`). No Makefile.
- `task build`: `CGO_ENABLED=0` static binary at `./loci`, version injected via `-ldflags -X`.
- `task test`: `go test -race ./...`.
- **No GoReleaser.** engRam builds with both Make and GoReleaser, which means two places where build flags are defined. Here `task release` does what GoReleaser does for engRam. If maintaining it becomes a burden, switch to GoReleaser rather than running both.
- **Releasing:** push a `vX.Y.Z` tag on `main`. `.github/workflows/release.yml` only installs Go and Task and runs `task release TAG=<tag>`, so the whole release is defined in the Taskfile:
  1. `check-tag`: the tag is `vX.Y.Z` and on `origin/main`. It runs before anything is published, so a bad tag publishes nothing.
  2. `test`, then `dist`: static binaries for darwin/linux × amd64/arm64 and windows/amd64 (engRam's set), as `loci_<os>_<arch>.tar.gz` (`.zip` on Windows) with LICENSE and README, plus `checksums.txt`. The version is the tag without its `v`.
  3. `formula`: `dist/loci.rb` from the checksums. It must pass `brew style`.
  4. `gh release create` with notes listing the commits since the previous tag, minus `docs`, `test` and `chore`.
  5. The formula is pushed to `Formula/loci.rb` in `AWDDude/homebrew-tap` (installed as `AWDDude/tap/loci`) through the contents API.
- `task dist TAG=vX.Y.Z` and `task formula TAG=vX.Y.Z` run locally without publishing. `task release` needs `GH_TOKEN` (release in this repo) and `TAP_GITHUB_TOKEN` (push to the tap), which CI takes from the `GITHUB_TOKEN` and the `TAP_GITHUB_TOKEN` repo secret.

## Conventions

- Module path: `github.com/AWDDude/loci`. License: MIT.
- Commits follow Conventional Commits.
