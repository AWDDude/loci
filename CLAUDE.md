# Loci

Entity-oriented persistent memory MCP server for coding agents. Single statically-linked Go binary. Experimental successor to engRam (`~/projects/AWDDude/engRam`), whose daemon and config handling are the reference implementation for the same concerns here.

**Status: design phase.** No code exists yet. README.md describes the intended design, and this file holds the rules the code must follow.

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
- **Edge** (entity → entity): from, edge type, to. Identified by that triple. Directional and stored once, and a symmetric type is stored in one canonical direction so `A spouse_of B` and `B spouse_of A` are the same edge. Queries from the `to` side report the inverse name.
  Types and inverses: `parent_of`/`child_of`, `spouse_of` (symmetric), `sibling_of` (symmetric), `member_of`/`has_member`, `owns`/`owned_by`, `works_on`/`worked_on_by`, `part_of`/`has_part`, `depends_on`/`depended_on_by`, `uses`/`used_by`, `related_to` (symmetric).

Entities and memories get a Loci-generated uuid, returned in every result. Links and edges have no uuid of their own: their identity is exact, so a uuid would only be a second name for the triple. For the same reason they need no search-before-create placeholder; a duplicate is detected exactly, and the fuzzy part (which entity) was already resolved by the search that produced the entity uuid. There is no edit history.

### What can change

- Entity name, aliases, description, and type: explicit edits only. Renaming does **not** add the old name as an alias. If the old name still matters, the caller adds the alias itself (an automatic alias would keep typo fixes like "Davd" around forever).
- Memory title and content: editable. Links: add and remove only, never retyped.
- Links and edges: create and delete only, both addressed by their triple. Creating one that already exists is a no-op that returns the existing record.

### Deletes

- Memory: removes its links.
- Edge or link: removes just that record.
- Entity: removes its edges. **Refused**, listing the memories, if any memory links only to that entity (deleting it would violate invariant 1).

## Search before create

Every search result ends with a "new" placeholder row for that kind. `entity_create` and `memory_create` require the placeholder's uuid. The placeholder uuid is regenerated on **every search of that kind and every create**, so a create only succeeds after the latest search. A stale uuid returns a short "search again" error. The create does not run a search itself or return candidates.

- Placeholders live in a metadata bucket, not as records, so they never appear in lists or counts.
- One placeholder per kind is global across all sessions. A search in session B invalidates session A's placeholder, so A has to search again. This is **accepted**: Loci serves one user on one workstation, and the retry is rare. Do not "fix" it with per-session or per-query tokens without revisiting the design.
- This is the pattern for anything that must not be duplicated.

Rejected alternatives: separate search tokens (the user dislikes passing tokens around); a stateful resolve → choose conversation with pending selections per session (it replaced this, being stateless and working across CLI invocations); `create` that searches and returns candidates (makes create double as a search tool).

## Names and matching

- Names and aliases may contain whitespace. On write, trim and collapse runs of whitespace to one space. Reject control characters and newlines.
- Casing is stored as given (`engRam`, `jq`) and never normalized.
- One case-folding function defines "same name". Search and uniqueness checks both call it. **Do not store a normalized key** next to the display name: the key is derived, so storing it only creates a way for the two to drift.

## Search and retrieval

- **No embeddings.** The project's hypothesis is that the agent supplies the semantics and Loci supplies the structure: find an entity, walk its edges, list linked memories filtered by link type.
- Entity search: trigram similarity plus BM25 over name, aliases, and description.
- Memory search: BM25 over title and content, as a fallback when the agent doesn't know which entity to start from.
- There are no memory-to-memory links and no tags. Both compete with entities as a way to organize, and the experiment needs one. If stale memories become a problem, the planned addition is a single `supersedes` relation.

## Stack

- **Storage:** bbolt, one file.
- **MCP:** mcp-go over stdio, proxied to a shared daemon.
- **CLI:** cobra. **Config:** viper.
- **Daemon:** as in engRam. The first client to need the database spawns a daemon that owns it, and every MCP session and every CLI command connects over a unix socket. The CLI never opens the bbolt file directly, since bbolt locks it to one process.
- **The socket speaks only MCP.** MCP sessions are a byte pipe between stdio and the socket. Each CLI command is an MCP client (mcp-go `transport.NewIO` over the socket) that makes one `tools/call` and formats the result, and `--json` prints the tool's structured result as is. Rejected: a second RPC protocol for the CLI, which would be a second wire format to keep in sync with the tools.

## CLI

- Human-readable output by default. Every command accepts `--json`.
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
├── daemon.lock
├── daemon.pid
└── daemon.log
```

The daemon's runtime files sit beside the database, so a config with a different data path gets its own daemon.

## Layout (proposed)

```
cmd/loci/           # entry point, cobra root
internal/config/    # viper loading and defaults
internal/daemon/    # socket, spawn, serve loop
internal/store/     # bbolt schema, trigram and BM25 indexes
internal/service/   # every capability, implemented once
internal/mcp/       # thin MCP tool adapters over service
internal/cli/       # thin cobra command adapters over service
```

## Build and release

- **Task is the only build entry point** (`Taskfile.yml`). No Makefile.
- `task build`: `CGO_ENABLED=0` static binary at `./loci`, version injected via `-ldflags -X`.
- `task test`: `go test -race ./...`.
- **No GoReleaser.** engRam builds with both Make and GoReleaser, which means two places where build flags are defined. Here a future `task release` will cross-compile, create the GitHub release with `gh release create`, and update the `AWDDude/tap` Homebrew formula, all run from CI. Releases are not set up yet. If maintaining the release task becomes a burden, switch to GoReleaser rather than running both.

## Conventions

- Module path: `github.com/AWDDude/loci`. License: MIT.
- Commits follow Conventional Commits.
