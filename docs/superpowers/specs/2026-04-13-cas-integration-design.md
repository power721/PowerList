# CAS Integration Design

## Summary

Integrate CAS workflow support into `PowerList` by aligning with the reference behavior from `/home/harold/workspace/OpenList-CAS`, with the following scope:

- `Local`: generate a same-name `.cas` file after uploading a regular file; optionally delete the original file after the `.cas` file is created.
- `189Cloud` and `189CloudPC`: generate a same-name `.cas` file after uploading a regular file; support uploading `.cas` files to restore the original file through provider-side rapid upload; optionally delete the `.cas` file after successful restore; optionally monitor configured directories and automatically restore existing `.cas` files in the background.

`Local` intentionally does not support restoring source files from `.cas` uploads and does not participate in background auto-restore.

## Goals

- Keep behavior aligned with the reference branch for the requested scope.
- Reuse a minimal shared CAS parsing layer across drivers.
- Avoid broad refactors of existing driver upload logic.
- Keep driver-specific restore code inside each driver, because the upstream APIs differ.

## Non-Goals

- No CAS restore support for `Local`.
- No generic CAS subsystem for all storage backends.
- No fallback behavior where a failed `.cas` restore silently uploads the `.cas` file itself.
- No changes to unrelated upload, copy, or cache infrastructure beyond the hook points needed for auto-restore.

## Confirmed Scope

### `Local`

- Add `generate_cas`
- Add `delete_source`
- After uploading a normal file, generate `<original_name>.cas` in the same directory.
- If `delete_source` is enabled, remove the original file only after the `.cas` file is successfully written.

### `189Cloud`

- Add `generate_cas`
- Add `delete_source`
- Add `restore_source_from_cas`
- Add `restore_source_use_current_name`
- Add `delete_cas_after_restore`
- Add `auto_restore_existing_cas`
- Add `auto_restore_existing_cas_paths`
- Support generating `.cas` after normal uploads.
- Support restoring the source file when the uploaded object is a `.cas` file.
- Support watching configured directories and auto-restoring `.cas` files that appear or change there.

### `189CloudPC`

- Add the same CAS-related options and behavior as `189Cloud`.

## High-Level Architecture

The implementation is split into four layers:

1. `internal/casfile`
   - Shared `.cas` parsing and validation only.
   - No storage-specific logic.

2. `drivers/local`
   - Upload-time hashing and `.cas` file generation for local filesystem writes.

3. `drivers/189` and `drivers/189pc`
   - Driver-specific `.cas` generation after successful uploads.
   - Driver-specific rapid restore from `.cas` payloads.
   - Driver-specific auto-restore orchestration.

4. Object update hook integration
   - Register auto-restore handlers for `189` and `189PC`.
   - Trigger background restore when watched paths receive `.cas` object updates.

This keeps common logic small while preserving the existing differences between `189Cloud` and `189CloudPC`.

## CAS File Format

The generated `.cas` file content matches the reference branch:

- JSON payload fields:
  - `name`
  - `size`
  - `md5`
  - `sliceMd5`
  - `create_time`
- The stored file content is base64-encoded JSON.

The parser must accept:

- Base64-encoded JSON payloads
- Raw JSON payloads

It must also accept either:

- `sliceMd5`
- `slice_md5`

The parser rejects payloads with:

- empty source name
- negative size
- missing `md5`
- missing `sliceMd5`/`slice_md5`

## Naming and Restore Rules

### Normal upload to `.cas`

For all supported generators, if the uploaded file is not itself a `.cas` file:

- Generate a `.cas` file with the exact name `<uploaded_name>.cas`
- Store it in the same destination directory

### Restore from uploaded `.cas`

For `189Cloud` and `189CloudPC`, if the uploaded file name ends with `.cas` and `restore_source_from_cas` is enabled:

- Parse the `.cas` payload
- Derive the restore target name
- Attempt provider-side rapid restore using the payload hashes and size
- Return the restored source object instead of uploading the `.cas` file itself

Restore target name resolution:

- If `restore_source_use_current_name=false`, use the payload `name`
- If `restore_source_use_current_name=true`, use the current uploaded `.cas` file name without the `.cas` suffix
- If the current name has no usable extension and the payload `name` has one, append the payload extension

Rejected restore names:

- empty name after trimming
- names containing `/` or `\`

## Hashing Rules

### `Local`

`Local` uses the reference branch behavior:

- Compute full-file MD5
- Compute per-slice MD5 with a fixed 10 MiB slice size
- If the file fits in one slice, `sliceMd5` equals the file MD5
- If the file spans multiple slices, `sliceMd5` is the MD5 of the newline-joined uppercase slice MD5 values

### `189Cloud` and `189CloudPC`

For generated `.cas` files:

- Reuse existing upload stream hashes when available
- Compute or derive the values needed to emit the same payload shape as the reference branch
- Keep provider-specific rapid-upload parameter formatting inside each driver

## Driver-Level Design

### `internal/casfile`

Create `internal/casfile/cas.go` with:

- `Info` struct for parsed CAS metadata
- `Parse([]byte) (*Info, error)`
- internal payload validation

Create `internal/casfile/cas_test.go` covering:

- base64 JSON parsing
- raw JSON parsing
- legacy `slice_md5`
- invalid payload rejection

### `drivers/local`

Modify `drivers/local/meta.go` to add:

- `GenerateCAS bool`
- `DeleteSource bool`
- `GenerateCASAndDeleteSource bool` as an internal compatibility toggle matching the reference branch style

Add `drivers/local/cas.go` with:

- `casUploadInfo`
- `casPayload`
- `casHasherWriter`
- helper methods:
  - `shouldUploadCAS`
  - `shouldDeleteSource`
  - `uploadCAS`
  - `deleteSource`
  - `updateDirSize`

Modify `drivers/local/driver.go` `Put` flow:

- write normal file to disk
- if CAS generation is enabled and the file is not a `.cas`, hash while writing
- once upload succeeds, write `<name>.cas`
- if configured, delete the source file only after `.cas` write succeeds

Add `drivers/local/cas_test.go` covering:

- normal upload generates `.cas`
- `delete_source` removes the original file after `.cas` creation

### `drivers/189`

Modify `drivers/189/meta.go` to add the CAS options from the confirmed scope.

Modify `drivers/189/driver.go` `Put` flow to branch as follows:

- normal file upload:
  - use the existing upload path
  - after success, generate and upload a same-name `.cas`
  - if configured, delete the original source object after `.cas` upload succeeds
- `.cas` upload with restore enabled:
  - parse the `.cas`
  - resolve restore name
  - call the 189 rapid-upload endpoints directly to restore the original object
  - return the restored source object semantics instead of uploading the `.cas` file itself

Add driver-specific CAS files:

- `drivers/189/cas.go`
- `drivers/189/cas_restore.go`
- `drivers/189/auto_restore.go`
- `drivers/189/auto_restore_hook.go`

Design constraints for `189`:

- Preserve the existing `newUpload` implementation as the primary normal upload path.
- Reuse existing listing, linking, and deletion methods instead of duplicating object access code.
- Use the existing provider session and rapid-upload endpoints already exposed by the driver utility layer.

### `drivers/189pc`

Modify `drivers/189pc/meta.go` to add the CAS options from the confirmed scope.

Modify `drivers/189pc/driver.go` `Put` flow to branch as follows:

- normal file upload:
  - use the existing method selection (`stream`, `rapid`, `old`)
  - after success, generate and upload a same-name `.cas`
  - if configured, delete the original source object after `.cas` upload succeeds
- `.cas` upload with restore enabled:
  - parse the `.cas`
  - resolve restore name
  - call the PC rapid-upload sequence using `initMultiUpload` and `commitMultiUploadFile`
  - return restored object semantics instead of uploading the `.cas` file itself

Add driver-specific CAS files:

- `drivers/189pc/cas.go`
- `drivers/189pc/cas_restore.go`
- `drivers/189pc/auto_restore.go`
- `drivers/189pc/auto_restore_hook.go`

Design constraints for `189PC`:

- Keep existing upload mode handling intact.
- Reuse current `findFileByName`, `Link`, `Remove`, `getFiles`, and cache invalidation helpers.
- Preserve family/personal branch behavior.

## Auto-Restore Design

Auto-restore is only implemented for `189Cloud` and `189CloudPC`.

### Configuration Semantics

- `auto_restore_existing_cas`
  - enables the background watcher logic
- `auto_restore_existing_cas_paths`
  - newline-separated relative paths under the current storage root
  - every configured path implicitly covers its subdirectories

### Triggering

- Register a handler through `op.RegisterObjsUpdateHook`
- When object updates arrive under a watched path:
  - filter to `.cas` files only
  - resolve the target directory object
  - start restore in a background context with timeout

### De-Duplication

Each driver keeps an in-flight map keyed by full `.cas` path so the same `.cas` file is not restored concurrently multiple times.

### Existing Source Behavior

If the restored source file already exists:

- skip the restore
- if `delete_cas_after_restore` is enabled, delete the `.cas` file anyway

### Cache Behavior

After restore or `.cas` deletion:

- clear the directory cache for the affected parent directory

### Error Behavior

- Auto-restore failures are logged
- They do not block the foreground upload/list flow
- Failure to delete the `.cas` file after a successful restore is logged as a warning only

## Failure Handling

### `.cas` generation failures

- The original file upload remains successful
- The API returns the error from `.cas` generation/upload
- `delete_source` must not run if `.cas` creation failed

### `.cas` restore failures

- Invalid payload returns a parse/validation error
- Missing source data on the provider returns a restore error
- The code must not silently upload the `.cas` file as fallback

### Illegal restore names

- Reject restore names containing path separators
- Reject empty names after removing `.cas`

## Testing Strategy

### Shared parser tests

Add unit tests for `internal/casfile`:

- parse base64 JSON
- parse raw JSON
- accept `slice_md5`
- reject invalid payloads

### `Local` tests

Add unit tests for `drivers/local`:

- generating `.cas` on normal upload
- deleting source after `.cas` creation

### `189` and `189PC` tests

Add logic-focused unit tests for:

- restore name resolution
- extension preservation when `restore_source_use_current_name=true`
- illegal restore name rejection
- path parsing for auto-restore watcher configuration

Where feasible, use driver utility stubs or response fixtures for rapid-upload flows instead of requiring live provider integration.

### Auto-restore tests

Add tests for:

- watched path parsing and de-duplication
- dispatching only `.cas` objects
- skip behavior when the target source file already exists

## File Impact

Expected new files:

- `docs/superpowers/specs/2026-04-13-cas-integration-design.md`
- `internal/casfile/cas.go`
- `internal/casfile/cas_test.go`
- `drivers/local/cas.go`
- `drivers/local/cas_test.go`
- `drivers/189/cas.go`
- `drivers/189/cas_restore.go`
- `drivers/189/auto_restore.go`
- `drivers/189/auto_restore_hook.go`
- `drivers/189pc/cas.go`
- `drivers/189pc/cas_restore.go`
- `drivers/189pc/auto_restore.go`
- `drivers/189pc/auto_restore_hook.go`

Expected modified files:

- `drivers/local/meta.go`
- `drivers/local/driver.go`
- `drivers/189/meta.go`
- `drivers/189/driver.go`
- `drivers/189/util.go`
- `drivers/189pc/meta.go`
- `drivers/189pc/driver.go`
- `drivers/189pc/utils.go`

## Rollout Order

1. Add shared `.cas` parser and tests
2. Add `Local` `.cas` generation and tests
3. Add `189PC` `.cas` generation and restore logic, then tests
4. Add `189` `.cas` generation and restore logic, then tests
5. Add auto-restore watcher support for `189PC`
6. Add auto-restore watcher support for `189`
7. Run targeted tests and a broader Go test pass for the affected packages

## Risks

- `189Cloud` and `189CloudPC` rapid-upload APIs are similar but not identical; over-sharing implementation would increase regressions.
- Auto-restore depends on object update hook timing and directory cache coherence; the implementation must invalidate the relevant parent directory cache after restore/delete.
- Returning errors after a successful original upload but failed `.cas` side-effect is behaviorally sharp; this is intentional to match the requested functionality, but it should be verified during implementation.
