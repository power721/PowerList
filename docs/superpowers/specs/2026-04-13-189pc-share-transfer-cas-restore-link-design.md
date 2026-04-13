# 189PC Share Transfer CAS Restore Link Design

## Summary

Adjust the `189CloudPC` share-transfer flow so that when a shared file is a `.cas`, the driver first transfers the `.cas` into the user's cloud drive, then restores the original source file from that transferred `.cas`, and finally returns the restored source file's download link instead of the `.cas` file link.

This behavior is specific to the share-transfer path and does not depend on the general `restore_source_from_cas` or `restore_source_use_current_name` configuration toggles.

## Goals

- Preserve the existing share-transfer path for non-`.cas` files.
- Force `.cas` restore for share-transfer results before returning a link.
- Return the restored source file link, not the transferred `.cas` link.
- Fail the request if `.cas` restore does not succeed.
- Keep the transferred `.cas` file in the temp directory after restore.

## Non-Goals

- No fallback to returning the `.cas` link after restore failure.
- No deletion of the transferred `.cas` file after restore.
- No change to the normal `Put` upload path.
- No change to name resolution for normal upload-time `.cas` restore.

## Confirmed Behavior

### Non-`.cas` share transfer

- Keep the current flow unchanged:
  - create share-save task into `TempDirId`
  - locate the transferred file
  - return `Link` for that transferred file

### `.cas` share transfer

- After the share-save task completes, locate the transferred `.cas` file in `TempDirId`.
- Read and parse the transferred `.cas` payload using the existing CAS parser path.
- Restore the source file into the same temp directory using provider-side rapid restore.
- Resolve the restore target name from the `.cas` payload `name` field only.
- If restore fails at any step, return an error immediately.
- If restore succeeds, return the `Link` for the restored source file.
- Leave the transferred `.cas` file in place.

## Design

### Entry point

Modify `drivers/189pc/extension.go` in `Transfer` so it no longer immediately returns `Link` for every transferred file. After locating `transferFile`, branch on whether the transferred object name ends with `.cas`.

### CAS-specific helper

Add a helper in `drivers/189pc/extension.go` dedicated to the share-transfer flow. It should:

- accept the current context and the transferred `.cas` object
- build a seekable stream from the transferred object using the existing `Link` + `stream.NewSeekableStream` path
- parse the `.cas` payload with `readCASRestoreInfo`
- force payload-name semantics by calling `restoreSourceFromCASInfo` on a temporary `Cloud189PC` view whose `RestoreSourceUseCurrentName` flag is false, regardless of storage config
- return `Link` for the restored object

This keeps the special-case behavior local to share transfer without changing the general upload path semantics.

### Failure handling

- If the transferred file cannot be found, keep returning the existing transfer error.
- If the transferred file is `.cas` and payload parsing fails, return that parse error.
- If rapid restore reports that source data is unavailable, return that restore error.
- If restore succeeds but `Link` for the restored object fails, return that link error.

### Testing

Add focused tests around the new helper behavior and the `.cas` branch selection in `Transfer`:

- non-`.cas` transfer still returns the transferred object link
- `.cas` transfer forces payload-name restore semantics
- `.cas` transfer propagates restore failure instead of returning a `.cas` link

The tests should avoid real network calls by isolating branching and restore-name enforcement into small overridable helpers.
