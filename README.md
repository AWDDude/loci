# Loci

An entity-oriented persistent memory MCP server for coding agents. Single statically-linked Go binary, no external services.

> **Status: experimental.** The first version works end to end but has not been released. Details may still change.

**Name:** from the *method of loci*, the memory-palace technique where you remember things by attaching them to places. In Loci, every memory must be attached to an entity before it can be stored.

Loci is an experimental successor to [engRam](https://github.com/AWDDude/engRam).

## The idea

Most agent memory stores are a pile of text searched by similarity. Loci instead organizes memory around **entities** (people, projects, repositories, tools, places, concepts) and the relationships between them.

The core hypothesis: **the agent supplies the semantics, Loci supplies the structure.** An agent already knows that "my son" means "the user's child", so Loci doesn't need an embedding model to work that out. It only needs to answer structured questions:

1. Search entities for "david" → David (person)
2. List David's edges of type `parent_of` → Noah (person)
3. List Noah's memories linked as `attribute` → "Noah's birthday is March 3"

A keyword search over memory text exists as a fallback for when the agent doesn't know which entity to start from. How often agents need that fallback is one of the things this experiment measures.

## Core model

Loci has three kinds of records and two kinds of relationships.

### Entities

A thing memories can be about. Each has a uuid, a name, optional aliases, a one-line description, and a type:

`person`, `organization`, `project`, `repository`, `service`, `tool`, `place`, `concept`

Names and aliases keep the casing you give them (`engRam`, `jq`, `iPhone`), but all matching is case-insensitive. Within a type, no two entities may share a name or alias, so "Mercury" the `project` and "Mercury" the `place` can coexist, but two `person` entities cannot both answer to "emma".

### Memories

A title and free-text content. A memory **must link to at least one entity** and may link to several.

### Links (memory → entity)

Each link says how the memory relates to that entity:

| Link type    | Meaning                                  | Example                                    |
|--------------|------------------------------------------|--------------------------------------------|
| `attribute`  | a fact about the entity                  | Emma's birthday                            |
| `preference` | how the entity wants things done         | David prefers `jq` for JSON                |
| `event`      | something that happened to or with it    | loci v1.0.0 was released                   |
| `decision`   | a choice made by it or about it          | loci uses bbolt instead of SQLite          |
| `mention`    | a loose association (the catch-all)      |                                            |

One memory can be an `event` for a project and a `decision` for a person at the same time.

### Edges (entity → entity)

Directional relationships between entities, stored once and readable from either end:

| Edge type    | Read from the other end |
|--------------|-------------------------|
| `parent_of`  | `child_of`              |
| `spouse_of`  | `spouse_of`             |
| `sibling_of` | `sibling_of`            |
| `member_of`  | `has_member`            |
| `owns`       | `owned_by`              |
| `works_on`   | `worked_on_by`          |
| `part_of`    | `has_part`              |
| `depends_on` | `depended_on_by`        |
| `uses`       | `used_by`               |
| `related_to` | `related_to`            |

Memories do not link to other memories. When a memory goes stale, it is edited or deleted.

## Search before you create

Duplicate entities ruin an entity-oriented store, so Loci makes it impossible to create one without first looking at what already exists.

Every entity search (and every memory search) ends with a **"new" placeholder row** that has its own uuid. To create a record, you pass the placeholder's uuid. The placeholder gets a fresh uuid after every search and every create, so a create only succeeds right after a search. If the uuid is stale, Loci just says to search again.

```
$ loci entity search david
  UUID      TYPE     NAME           DESCRIPTION
  3f9c...   person   David Kittle   the user, owner of these repos
  a71e...   service  david-ops      deploy bot
  c02b...   (new)    create a new entity
```

Using an existing entity means using its uuid, and creating a new one means using the `(new)` row's uuid.

## Agents and humans get the same interface

Everything an agent can do through MCP, a human can do through the `loci` CLI, and the reverse. Output is human-readable by default, and every command takes `--json`.

## Architecture

- **Storage:** [bbolt](https://github.com/etcd-io/bbolt), one file at `~/.local/share/loci/loci.bbolt`
- **Search:** trigram and BM25 matching, no embedding model
- **MCP:** [mcp-go](https://github.com/mark3labs/mcp-go) over stdio
- **CLI and config:** [cobra](https://github.com/spf13/cobra) and [viper](https://github.com/spf13/viper)
- **Concurrency:** like engRam, the first session starts a shared background daemon that owns the database, and every MCP session and CLI command talks to it over a unix socket.

## Configuration

Optional, at `~/.config/loci/config.yaml` (honors `XDG_CONFIG_HOME`). Any key can be overridden with a `LOCI_*` environment variable, or the whole file can be replaced with `--config`. Without a config file, Loci runs on defaults.

## Building

Requires Go and [Task](https://taskfile.dev).

```bash
task build   # static binary at ./loci
task test
```

## License

[MIT](LICENSE)
