---
name: Publish/Registry Review 2026-04-04
description: Publish command review findings: fetch drops hash_id when indexing, project-mode skips artifactdb, isManifestNotFoundError too broad
type: project
---

## Review: Publish Command + Registry + Artifact DB Integration

### Critical bugs found:
1. **Fetch drops hash_id/build_tags when indexing manifest-fetched artifacts** (cmd/fetch.go:174-181). The `indexSuccessfulArtifact` call omits `HashID` and `BuildTags` from the manifest `artifact` variable that is available in scope. This defeats hash-based artifact lookup.
2. **Project-mode publish skips artifactdb indexing** (cmd/publish.go:164-178). The `if localMode` guard means `cstow publish` (zero args) never records the published artifact in the local DB.

### Warnings:
3. **`isManifestNotFoundError` matches "not found" substring broadly** (cmd/publish.go:271-282), could swallow permission errors. Should use `types.NoSuchKey` from AWS SDK.
4. **Same issue with `isArtifactNotFoundError`** (cmd/fetch.go:529-535).
5. **Relative publishDir** (`build/release`) stored in artifactdb for project-mode publish would break cross-directory lookups.
6. **No upload integrity verification** after S3 PutObject.

### What works correctly:
- S3 upload key layout is symmetric with download key fallback chain
- Manifest merge/dedup by (abi_tag, build_type) is correct
- TOML round-trip of hash_id and build_tags confirmed by tests
- Artifactdb schema migration preserves hash_id on re-upsert with empty values
- SelectArtifact fallback logic (exact build_type -> legacy untyped) is sound
