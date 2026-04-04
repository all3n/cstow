# Artifact Hash ID Publish Design

## Goal

Extend registry publishing and artifact inspection so `cstow` can:

- publish an already-built local artifact directly from the local cache
- assign each published artifact a stable content-derived `hash_id`
- persist that `hash_id` in both the local artifact database and remote manifest metadata
- allow later lookup and fetch by `hash_id`

This work must preserve the existing project-directory `cstow publish` flow while adding a second "publish from local artifact" flow.

## Current Context

Today `cstow publish` only works from a project directory containing `cstow.toml` plus `build/release` or `build/debug`. The local artifact cache and local artifact database exist, but publish cannot take a package name/version/ABI/build-type tuple and turn that cached install prefix into a registry artifact. Remote artifact identity is still primarily `(name, version, abi_tag, build_type)`.

The repository now already has:

- build-type-aware cache layout
- local artifact SQLite indexing
- registry manifest support
- shared-config-aware registry endpoint resolution

The missing piece is a stable content-based artifact identifier that can bridge local cache records and remote registry objects.

## Non-Goals

This design does not add a new remote global artifact index file or service.

This design does not change the canonical cache install layout. Downloaded artifacts still unpack under:

`~/.cstow/cache/<name>/<version>/<abi_tag>/<build_type>/`

This design does not add complex query expressions for build tags. Tag filtering stays as exact `key=value` membership checks.

## Artifact Identity

Each published artifact gets:

- `sha256`: full SHA-256 of the uploaded `.tar.zst` bytes
- `hash_id`: initially equal to the full `sha256` hex string

`hash_id` is content-based, not metadata-based. If two uploads produce identical archive bytes, they will share the same `hash_id`. This gives stable deduplication semantics and lets the same value serve as both a reference token and a verification handle.

CLI input may later accept unique prefixes of `hash_id`, but persistent storage and manifests will always store the full 64-character hex value.

## CLI Changes

### Publish

Keep the existing project mode:

```bash
cstow publish
```

Add a local-artifact mode:

```bash
cstow publish <name> \
  --version <version> \
  --abi-tag <abi_tag> \
  --build-type <build_type> \
  --build-tag key=value \
  --build-tag key=value
```

Behavior:

- if `<name>` is omitted, publish behaves exactly like today and packages the current project build directory
- if `<name>` is present, publish resolves a local artifact from the artifact database first, then falls back to the cache layout
- the selected local install prefix is packaged into a `.tar.zst`
- the archive bytes are hashed to produce `sha256` and `hash_id`
- the artifact is uploaded to the registry using the new object key layout
- the manifest is updated to include the new artifact metadata
- the local artifact database row is updated with `hash_id` and `build_tags`

### Artifact Inspection

Add:

```bash
cstow artifact show <hashid>
```

Behavior:

- looks up the local SQLite artifact database by full or unique-prefix `hash_id`
- prints the resolved package identity and install directory
- prints all stored build tags and origin metadata
- returns an ambiguity error if the prefix matches more than one row

### Fetch

Add:

```bash
cstow fetch --artifact <hashid>
```

Behavior:

- tries local artifact database lookup first
- if a matching local install directory still exists, treat it as an immediate cache hit
- otherwise resolve the artifact from remote manifests and download it
- unpack to the standard cache path `<name>/<version>/<abi_tag>/<build_type>/`
- update the local artifact database with the resolved `hash_id`

Remote `hash_id` fetch initially uses package/version manifest scanning, not a separate remote hash index.

## Build Tags

`build_tag` is a repeatable CLI flag whose value must be `key=value`.

Examples:

```bash
--build-tag compiler=gcc11
--build-tag profile=release
--build-tag libcxx=libstdc++
```

Persistent representation:

- manifest: string array
- SQLite: JSON-encoded string array

Matching semantics:

- artifact query filters require the artifact to contain all requested tags
- no inequality or wildcard syntax is introduced

## Remote Object Layout

Current object layout:

`<prefix>/<name>/<version>/<abi_tag>/<build_type>.tar.zst`

New object layout:

`<prefix>/<name>/<version>/<abi_tag>/<build_type>/<hash_id>.tar.zst`

This keeps package/version/ABI/build-type browsing readable while making the final path segment a stable content-derived reference.

Backward compatibility:

- download logic should continue to support existing legacy object keys
- manifest artifact selection should prefer explicit `hash_id` records when available

## Manifest Format

Each `[[artifact]]` entry gains:

- `hash_id`
- `build_tags`

Example:

```toml
[[artifact]]
abi_tag = "gcc11-cxx17-libstdc-linux-x86_64"
build_type = "shared"
hash_id = "be6226edb15c0a5e37a15a65f9cf808f8973b8bc60cdf3882fc407f881c00f33"
sha256 = "be6226edb15c0a5e37a15a65f9cf808f8973b8bc60cdf3882fc407f881c00f33"
size = 158
build_tags = ["compiler=gcc11", "profile=release"]
```

Manifest selection rules become:

- existing ABI/build-type matching remains
- when fetching by `hash_id`, remote resolution searches manifest artifacts for exact `hash_id`
- when fetching by ABI/build-type, `hash_id` is preserved as metadata but is not required for match selection

## Local Artifact Database Changes

Extend `artifacts` rows with:

- `hash_id TEXT`
- `build_tags TEXT`

Keep the existing primary key `(name, version, abi_tag, build_type)` because cache layout still uses that identity.

Add a unique index on `hash_id` when it is present.

Population rules:

- `publish` writes `hash_id` and `build_tags`
- remote download paths write `hash_id` if manifest metadata includes it
- `artifact sync` may leave `hash_id` empty because a raw cache scan does not know the original packaged archive bytes

## Resolution Rules

### Publish Local Artifact Mode

Inputs:

- `name`
- `version`
- `abi_tag`
- `build_type`
- optional repeated `build_tag`

Lookup order:

1. local artifact database exact match on `(name, version, abi_tag, build_type)`
2. standard cache layout fallback

If more than one local row is later allowed for the same tuple due to build tags, tag filters narrow the result set. For this first iteration, build tags are metadata recorded on publish and fetch; they do not redefine cache layout.

### Artifact Show

Lookup order:

1. exact `hash_id`
2. unique `hash_id` prefix

If zero matches: not found.

If multiple prefix matches: return an ambiguity error and show candidate full IDs.

### Fetch By Hash ID

Lookup order:

1. local artifact database exact/prefix match
2. remote manifest scan for matching `hash_id`

The command may require package/version hints internally during the first iteration if there is no remote global hash index. That is acceptable as an implementation detail, but the user-facing target remains `--artifact <hashid>`.

## Testing Requirements

Required automated coverage:

- project publish still works
- local artifact publish mode packages the cache install prefix correctly
- `hash_id` is derived from content bytes and written to manifest
- remote object key uses the `.../<build_type>/<hash_id>.tar.zst` layout
- local artifact database stores and returns `hash_id`
- `artifact show <hashid>` resolves exact and unique-prefix matches
- ambiguous prefix lookup fails clearly
- `fetch --artifact <hashid>` reuses a local hit when present
- `fetch --artifact <hashid>` can resolve and download from manifest metadata

## Risks

- current manifest upload code overwrites the version manifest, so manifest merge behavior must be handled carefully when adding new artifacts
- cache scan cannot reconstruct `hash_id`, so `artifact sync` remains lossy for hash metadata
- remote `hash_id` lookup without a global remote index may be slower because it depends on manifest scanning

## Recommended Implementation Order

1. extend manifest and SQLite schema
2. add local-artifact publish mode and `hash_id` object keys
3. add `artifact show <hashid>`
4. add `fetch --artifact <hashid>`
5. verify with unit tests plus a real registry smoke test
