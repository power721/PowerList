# Thunder Share CAS Playback Design

## Summary

Add transparent playback support for `.cas` files stored in `ThunderShare`. When a user requests a link for a shared `.cas` file, PowerList should read and parse the CAS metadata through an available `ThunderBrowser` account, use an available `189CloudPC` account to restore the original payload through provider-side rapid upload, and return the restored 189CloudPC file link.

The original file in the Thunder share is metadata only and must remain untouched. Playback succeeds only when 189CloudPC accepts the hashes and size from the CAS payload for rapid restore.

## Goals

- Make `.cas` files in `ThunderShare` playable through the normal `Link` path.
- Keep ordinary Thunder share playback unchanged.
- Reuse the existing CAS parser and 189CloudPC restore implementation.
- Try distinct configured 189CloudPC accounts until one restores and links the payload successfully.
- Clean up the temporary Thunder CAS copy and the restored 189CloudPC payload after the configured delay.
- Return errors that identify the failed stage instead of returning an unplayable `.cas` link.

## Non-Goals

- Do not treat `.cas` as a media format.
- Do not upload the `.cas` file into 189CloudPC before restoring its payload.
- Do not modify or delete the original `.cas` object in the Thunder share.
- Do not add a generic cross-storage CAS orchestration subsystem.
- Do not change normal Thunder share link caching.
- Do not add persistent caching for restored CAS playback links.
- Do not guarantee that every valid CAS payload can be restored; the source data must be accepted by 189CloudPC rapid upload.

## User-Facing Behavior

### Ordinary Thunder share file

- Preserve the existing flow: save the shared file into a ThunderBrowser temporary directory, obtain its Thunder link, cache the result by shared file ID, and schedule deletion of the temporary Thunder file.

### Thunder share `.cas` file

1. Save the shared `.cas` file into a ThunderBrowser temporary directory.
2. Obtain a link for the temporary Thunder object.
3. Read and parse the CAS metadata with a strict metadata size limit.
4. Try the preferred 189CloudPC account first, followed by the remaining distinct 189CloudPC accounts.
5. For each account, reuse an existing same-named payload in its restore temp directory or restore the payload through provider-side rapid upload.
6. Return the first successfully generated 189CloudPC payload link.
7. Schedule delayed deletion of the temporary Thunder `.cas` copy and the restored or reused 189CloudPC payload.
8. Do not cache the resulting CAS playback link in `thunderShareLinkCache`.

If every 189CloudPC account fails, return an error. Never fall back to the Thunder `.cas` download link.

## Approaches Considered

### Recommended: orchestrate from `ThunderShare`

`ThunderShare` reads the metadata and asks `189CloudPC` to restore from the parsed CAS information. This performs no unnecessary cross-provider upload and keeps provider responsibilities clear:

- ThunderShare owns access to the shared metadata file.
- `internal/casfile` owns CAS parsing and validation.
- 189CloudPC owns rapid restore, payload linking, and its own temporary-file cleanup.

### Upload the CAS file to 189CloudPC first

PowerList could download the Thunder `.cas`, upload it into 189CloudPC, and invoke the existing 189CloudPC `Link` behavior. This reuses the current public link path but adds an unnecessary upload, another temporary object, more cleanup states, and more failure points.

### Add a generic cross-storage CAS service

A generic service could read CAS metadata from any source storage and dispatch it to any compatible restore provider. This would be extensible but is broader than the current Thunder-to-189CloudPC requirement and would introduce interfaces without a second concrete consumer.

## Architecture

### `internal/casfile`

Add a reader-based parsing helper with a maximum content size. Generated CAS metadata is small, so the playback path should reject input larger than 1 MiB instead of reading an arbitrarily large shared object into memory.

The helper should:

- accept an `io.Reader`
- read no more than 1 MiB plus one detection byte
- return a specific metadata-too-large error when the limit is exceeded
- delegate accepted content to the existing `Parse` function

Existing `Parse([]byte)` behavior and accepted payload formats remain unchanged.

### `drivers/189pc`

Expose a focused playback restore method that accepts an already parsed `casfile.Info` and a CAS file name. The method should encapsulate all 189CloudPC-specific behavior:

- force payload-name semantics regardless of `RestoreSourceUseCurrentName`
- resolve the existing personal or family restore temp directory
- reject invalid payload names through the existing name validation
- reuse a same-named payload object when one already exists
- otherwise call the existing provider-side rapid restore flow
- create a direct link for the selected payload object
- schedule delayed cleanup of that payload object

Refactor the existing 189CloudPC `.cas` playback path to call the same metadata-based helper after opening and parsing its local CAS object. The upload-time and auto-restore paths keep their current configuration semantics and are not changed to use forced playback naming.

The cleanup context used by the playback helper must ignore request cancellation so cleanup still runs after the client disconnects.

### `drivers/thunder_share`

Branch on the file suffix before consulting `thunderShareLinkCache`:

- non-`.cas` files keep the existing cached resolver
- `.cas` files call a dedicated CAS playback resolver and bypass the ordinary link cache

The CAS resolver has two phases.

#### Phase 1: read CAS metadata through Thunder

- Try available ThunderBrowser accounts using the existing share-save and direct-link behavior.
- Schedule deletion of the saved Thunder temporary object with a cancellation-independent context.
- Open the returned link as a stream using the shared file's name and size metadata.
- Parse the stream through the bounded reader helper in `internal/casfile`.
- Stop and return the real Thunder or parsing error if no account can provide valid CAS metadata.

#### Phase 2: restore through 189CloudPC

- Build a distinct candidate list with the configured preferred 189CloudPC account first, followed by all remaining configured 189CloudPC storages.
- Call the new metadata-based playback restore method on each candidate.
- Return immediately when an account restores and links the payload successfully.
- Record the account ID and error for failed candidates.
- If every candidate fails, return a combined error preserving the individual failure information.

## Data Flow

```text
ThunderShare.Link(.cas)
  -> Thunder share save into ThunderBrowser temp directory
  -> Thunder direct link
  -> bounded CAS read and parse
  -> try 189CloudPC account candidates
       -> find or rapid-restore payload in 189 temp directory
       -> create 189 payload link
       -> schedule 189 payload cleanup
  -> schedule Thunder temp CAS cleanup
  -> return 189 payload link
```

The original Thunder share object is read-only throughout this flow.

## Naming Semantics

- Use the CAS payload `name` field for playback restore.
- Ignore the target 189CloudPC storage's `RestoreSourceUseCurrentName` setting for this path.
- Reject empty names and names containing `/` or `\\` through the existing 189CloudPC restore-name validation.
- Do not derive the payload name from the Thunder share object's current `.cas` filename.

This matches the existing 189CloudPC direct CAS playback and transferred-share CAS playback behavior.

## Cache and Concurrency

Ordinary Thunder share files retain the existing one-hour link cache.

CAS playback links must bypass that cache because the returned link refers to a temporary 189CloudPC payload that may be deleted before the cache entry expires. A later playback request should repeat metadata parsing and restore or reuse a current payload instead of returning a stale link.

No persistent CAS playback cache or new concurrency lock is required. Concurrent requests for the same shared CAS may both attempt restore. They must not modify the original share object, and provider errors should be returned normally.

## Temporary File Cleanup

Two independent temporary objects may be created:

- the saved `.cas` object in the ThunderBrowser temp directory
- the restored or reused payload in the 189CloudPC restore temp directory

Cleanup rules:

- use the existing global `conf.DeleteDelayTime` setting
- skip cleanup when the configured delay is zero
- use `context.WithoutCancel` or an equivalent cancellation-independent context
- delete only the temporary objects owned by each provider
- never delete the original Thunder share object
- treat delayed cleanup failures as log-only after a link has already been returned

## Error Handling

- If no ThunderBrowser account is available, return the existing missing-Thunder-account error.
- If Thunder share save or link creation fails, retry according to the existing Thunder account selection behavior and return the real final error.
- If CAS content is empty, malformed, invalid, or larger than 1 MiB, return a parsing or size-limit error and do not call 189CloudPC.
- If no 189CloudPC account is configured, return `找不到天翼云盘帐号`.
- If one 189CloudPC account fails to restore or link, continue with the next distinct candidate.
- If all 189CloudPC candidates fail, return a combined error containing the relevant account IDs and causes.
- If 189CloudPC reports that source data is unavailable, preserve a clear error explaining that the CAS payload could not be rapid-restored.
- Never return the Thunder `.cas` link as playback fallback.

## Testing

### `internal/casfile`

- reader helper parses valid raw JSON and base64 CAS payloads
- reader helper rejects content larger than 1 MiB
- reader helper preserves existing empty and malformed payload errors

### `drivers/189pc`

- metadata-based playback restore forces payload-name semantics
- existing payload is reused before rapid restore
- missing payload is restored and linked
- the payload object, not the CAS metadata object, is scheduled for cleanup
- cleanup context survives cancellation of the playback request
- existing direct 189CloudPC `.cas` playback uses the shared helper

### `drivers/thunder_share`

- ordinary files keep using the current resolver and cache
- `.cas` files bypass reads and writes to `thunderShareLinkCache`
- valid Thunder CAS metadata is passed to 189CloudPC and returns the 189 link
- a failed preferred 189CloudPC account is followed by another distinct account
- no 189CloudPC accounts returns a clear error
- invalid, empty, and oversized CAS input prevents restore attempts
- all-account restore failure preserves the actual causes
- the saved Thunder CAS object is the Thunder cleanup target
- cancellation of the request does not cancel delayed Thunder cleanup

Use overridable function seams for provider selection, Thunder save/link, stream opening, CAS parsing, 189CloudPC restoration, and cleanup scheduling so unit tests remain deterministic and make no network calls.

## Verification

Run the focused package tests:

```bash
go test ./internal/casfile ./drivers/189pc ./drivers/thunder_share
```

Then run the full regression suite:

```bash
go test ./...
```
