# 189PC CAS Play Restore Design

## Summary

Add direct-play support for existing `.cas` files in `189CloudPC`. When a user requests a link for a `.cas` file that already exists in the mounted cloud drive, the driver should restore the original source file into the same temporary location used by share-play restore, return the restored file's playable link, and schedule delayed deletion of the restored file using the same cleanup behavior already used by share-play restore.

The original `.cas` file must remain in place.

## Goals

- Make existing `.cas` files playable through the normal `Link` path.
- Keep non-`.cas` link behavior unchanged.
- Reuse the existing CAS parsing and restore flow.
- Reuse the same delayed cleanup semantics used by share-play restore.
- Preserve the `.cas` file after playback restore.

## Non-Goals

- No attempt to make `.cas` itself a playable media format.
- No new storage configuration for cleanup delay.
- No change to background auto-restore watcher semantics.
- No fallback to returning a `.cas` download link when restore fails.
- No new concurrency lock beyond existing behavior.

## User-Facing Behavior

### Non-`.cas` file

- `Link` returns the direct file link exactly as it does today.

### Existing `.cas` file in cloud drive

1. Open the `.cas` file stream through the normal link path.
2. Parse CAS payload metadata.
3. Resolve the restore target name from the CAS payload name only.
4. If a same-named source file already exists in the restore temp directory, reuse it.
5. Otherwise restore the source file into the restore temp directory.
6. Return the playable link for the reused or restored source file.
7. Schedule delayed deletion for that source file using the same delay and async cleanup pattern used by share-play restore.
8. Keep the original `.cas` file untouched.

## Architecture

### `Link` branching

Modify `drivers/189pc/driver.go` so `Link` no longer treats every file the same. Add a small branch:

- non-`.cas` files continue through the current direct-link logic
- `.cas` files go through a dedicated helper that restores or reuses the source file in the restore temp directory before linking it

This keeps the public entry point correct while avoiding large behavior changes to ordinary link generation.

### CAS play helper

Add helper seams around the existing direct-link code and new CAS play flow, similar to the share-transfer implementation in `drivers/189pc/extension.go`.

The CAS play helper should:

- accept the current context and the `.cas` object
- open a seekable stream for the `.cas` object
- parse CAS metadata using `readCASRestoreInfo`
- force payload-name semantics by cloning the driver and setting `RestoreSourceUseCurrentName = false`
- locate the restore destination as the same temp directory already used by share-play restore
- check whether the restored source file already exists in that temp directory
- if found, reuse that object
- otherwise call `restoreSourceFromCASInfo`
- link the chosen source object using the normal link path
- schedule delayed deletion for the chosen source object
- return the source link

### Cleanup

Reuse the delayed cleanup behavior already present in `Transfer`:

- read the global delete delay from `conf.DeleteDelayTime`
- if delay is `0`, skip cleanup
- delete only the restored or reused source object
- never delete the original `.cas` file
- treat cleanup failure as log-only

The cleanup logic should be factored into a small helper so `Transfer` and the new CAS play path can share one deletion implementation instead of duplicating async removal code.

## Naming Semantics

Existing play-restore must match share-play restore semantics, not upload-time restore semantics.

- Ignore the storage's `RestoreSourceUseCurrentName` value for this path.
- Always use the CAS payload `name` field when choosing the restore target name.
- Reject payload names that contain a path, consistent with existing restore validation.

## Error Handling

- If opening the `.cas` stream fails, return that error.
- If parsing CAS metadata fails, return that error.
- If restore-name resolution fails, return that error.
- If restore cannot find source data in 189CloudPC, return that error.
- If linking the restored source object fails, return that error.
- Do not fall back to the `.cas` object's direct link after any restore-path failure.
- If delayed deletion fails, log it and do not affect the returned link.

## Concurrency

No new in-flight deduplication is required for this change.

If multiple play requests target the same `.cas` file concurrently, they may race between reusing an existing restored source file and restoring again. This matches the current pragmatic behavior of related CAS flows and is acceptable for this scope.

## Testing

Add focused tests around the new link branching and cleanup scheduling:

- non-`.cas` `Link` still uses the direct-link path
- `.cas` `Link` opens, parses, restores, links, and schedules cleanup
- `.cas` `Link` forces payload-name semantics even when `RestoreSourceUseCurrentName` is enabled on the storage
- `.cas` restore failure returns an error and does not fall back to direct `.cas` linking
- existing same-named source file is reused instead of restored again
- cleanup target is the restored or reused source file, never the `.cas` file

Use test seams for direct link generation, CAS stream opening, CAS parsing, source restore, object lookup, and cleanup scheduling so the tests stay local and deterministic.
