# GuangYaPan Driver Design

## Summary

Integrate `GuangYaPan` into `PowerList` by porting the full upstream driver behavior from the referenced Alist commit into the OpenList v4 codebase.

Scope includes:

- two-stage SMS login
- access token validation and refresh token renewal
- file listing and download link resolution
- mkdir, rename, remove, move, copy
- upload via GuangYaPan resource-center token, Aliyun OSS multipart upload, and task polling

The goal is behavior parity with the reference implementation, adapted to this repository's driver interfaces and persistence patterns.

## Goals

- Add a new `GuangYaPan` storage driver that is usable from the existing storage admin UI.
- Preserve the reference login and upload behavior closely enough that existing GuangYaPan accounts can be configured with the same field model.
- Fit the implementation into the repository's v4 driver contracts, especially `Put(...)(model.Obj, error)` and storage status persistence.
- Keep the initial implementation self-contained and avoid unrelated refactors.

## Non-Goals

- No redesign of the GuangYaPan flow beyond what is required for v4 compatibility.
- No new shared token framework for other drivers.
- No frontend UI changes outside normal driver metadata exposure.
- No full end-to-end CI coverage against the real GuangYaPan service.

## Confirmed Scope

The new driver will expose these configuration fields:

- `root_folder_id`
- `phone_number`
- `captcha_token`
- `send_code`
- `verify_code`
- `verification_id`
- `access_token`
- `refresh_token`
- `client_id`
- `device_id`
- `page_size`
- `order_by`
- `sort_type`

The runtime behavior will support:

- login priority: `access_token` -> `refresh_token` -> SMS login
- SMS two-step flow:
  - save with `send_code=true` to request SMS and persist `verification_id`
  - save with `verify_code` to complete login and persist tokens
- list/read/write operations on GuangYaPan files
- instant-upload handling when the upstream API returns a completed task path
- multipart OSS upload when instant upload is unavailable

## High-Level Architecture

The implementation stays within a single new driver package:

1. `drivers/guangyapan/meta.go`
   - driver registration
   - addition struct
   - config declaration and operator guidance

2. `drivers/guangyapan/types.go`
   - GuangYaPan account API response structs
   - GuangYaPan file API response structs
   - upload token and task polling response structs
   - small local helpers such as Unix timestamp conversion

3. `drivers/guangyapan/driver.go`
   - driver lifecycle
   - login and token management
   - file operations
   - upload orchestration
   - normalization helpers

4. `drivers/all.go`
   - blank import registration

This matches the reference implementation closely and minimizes integration risk.

## Driver Design

### Driver Type

`GuangYaPan` will embed:

- `model.Storage`
- `Addition`

and hold two `resty.Client` instances:

- account client for `account.guangyapan.com`
- API client for `api.guangyapan.com`

The driver will implement:

- `driver.Driver`
- `driver.Mkdir`
- `driver.Rename`
- `driver.Move`
- `driver.Copy`
- `driver.PutResult`

Mutation methods other than upload will use the plain error-returning interfaces because the GuangYaPan APIs for those operations are task-oriented and do not naturally return full object metadata. `Put` will use `driver.PutResult`.

### Initialization

`Init` will:

- normalize `client_id`, `device_id`, paging, and sort parameters
- trim stored login fields
- build the two HTTP clients with GuangYaPan-specific headers
- try login in priority order:
  - validate `access_token`
  - refresh using `refresh_token`
  - complete SMS login when `phone_number + verify_code` are present
  - when `phone_number` exists and `send_code=true`, attempt to send the SMS code and persist the resulting state without failing the entire driver load unnecessarily

This mirrors the reference behavior where “send code” is an admin workflow, not a permanent operational mode.

## Login and Token Flow

### Access Token Validation

Validation uses the account endpoint `/v1/user/me` with bearer auth. A missing or invalid subject means the token is unusable.

### Refresh Token

Refresh uses `/v1/auth/token` with:

- `grant_type=refresh_token`
- `client_id`
- `refresh_token`

On success:

- update `access_token`
- update `refresh_token` when the response supplies one
- persist storage through `op.MustSaveDriverStorage`

### SMS Login

The SMS flow has three logical pieces:

1. captcha token acquisition through `/v1/shield/captcha/init`
2. verification request through `/v1/auth/verification`
3. code verification and signin through:
   - `/v1/auth/verification/verify`
   - `/v1/auth/signin`

Driver behavior:

- if `captcha_token` is empty during an explicit send-code action, initialize one
- if verification request reports an expired or invalid captcha token, refresh the captcha token once and retry
- on successful signin:
  - persist `access_token`
  - persist `refresh_token`
  - clear one-time fields such as `verify_code` and `verification_id`

### Storage Status

Because the repository’s storage initialization flow overwrites status after `Init`, SMS send feedback will use delayed status persistence in the same style as other drivers that need user-visible asynchronous state messages.

## File Operations

### List

`List` will call `/nd.bizuserres.s/v1/file/get_file_list` page by page using:

- parent ID
- page number
- page size
- sort parameters

The root directory behavior will treat configured `root_folder_id` as the mounted root and send an empty upstream parent ID when listing that root.

Each entry maps to `model.Object` with:

- `ID`
- `Path`
- `Name`
- `Size`
- `Modified`
- `Ctime`
- `IsFolder`

### Link

`Link` will call `/nd.bizuserres.s/v1/get_res_download_url` and prefer:

- `signedURL`
- fallback to `downloadUrl`

Directories return `errs.NotFile`.

### Mutations

The driver will implement:

- `MakeDir` via `/nd.bizuserres.s/v1/file/create_dir`
- `Rename` via `/nd.bizuserres.s/v1/file/rename`
- `Remove` via `/nd.bizuserres.s/v1/file/delete_file`
- `Move` via `/nd.bizuserres.s/v1/file/move_file`
- `Copy` via `/nd.bizuserres.s/v1/file/copy_file`

For delete, move, and copy, the API may return a task ID. When present, the driver will poll task status until completion or failure.

## Upload Design

### Upload Flow

`Put` will follow this sequence:

1. ensure authentication
2. validate file metadata
3. request an upload token from `/nd.bizuserres.s/v1/get_res_center_token`
4. branch on response code
   - if instant upload is accepted, wait on task completion only
   - otherwise upload file contents to OSS
5. poll `/nd.bizuserres.s/v1/file/get_info_by_task_id` until the uploaded file appears
6. return the resolved uploaded object when possible

### Upload Token Compatibility

The reference driver contains compatibility fixes that must be preserved:

- accept provider fields with unstable typing
- backfill top-level credentials from nested `creds`
- normalize endpoint fields when only host-style data is returned
- handle status codes used by GuangYaPan for in-progress upload tasks

These compatibility behaviors are part of the required scope, not cleanup.

### OSS Multipart Upload

Upload implementation will:

- create an OSS client using temporary credentials from GuangYaPan
- normalize the OSS endpoint so the OSS SDK receives a service endpoint rather than a bucket-prefixed host
- choose part size by file size using the same thresholds as the reference implementation
- use `driver.NewLimitedUploadStream` so cancellation and server upload limiting still apply
- update progress after each uploaded part

Zero-byte files use a simple object put instead of multipart upload.

### Returned Object Semantics

After upload task polling completes, the driver will return a `model.Object` for cache population. The object will include:

- uploaded file name
- file size
- resolved file ID when task-info returns one
- temporary or stream-derived timestamps when the upstream response does not provide exact values

This removes ambiguity at the v4 writer boundary while keeping the implementation lightweight.

## Error Handling

Errors should remain close to the upstream semantics:

- prefer service-provided message fields over generic HTTP status text
- retry authenticated API calls once after a successful refresh when the original request returns `401` or `403`
- fail fast on empty critical fields such as missing file IDs, file names, tokens, or download URLs
- distinguish task timeout from explicit task failure

SMS-send initialization is the one exception where the driver may keep the storage load alive while persisting actionable status text for the operator.

## Testing Strategy

The initial test set will cover deterministic local logic only.

Add `drivers/guangyapan/driver_test.go` with tests for:

- `normalizeDeviceID`
- `normalizePhoneE164`
- `normalizeCaptchaUsername`
- `normalizeOSSEndpoint`
- `calcUploadPartSize`
- `unixOrZero`

No real-network tests are planned for this first pass because:

- this repository generally does not run real third-party cloud login tests in CI
- GuangYaPan login requires live SMS/captcha state
- OSS upload tests would need live temporary credentials from GuangYaPan

## Risks and Mitigations

- API instability: preserve the reference compatibility logic and avoid premature cleanup.
- Login fragility: keep the same field model and request headers as the reference commit.
- v4 interface mismatch: adapt writer methods carefully and verify with compile-time checks.
- Storage status races: use delayed status persistence only where needed and leave normal runtime status handling untouched.

## Implementation Plan Boundary

This design covers one implementation unit:

- add and register the new driver
- adapt it to v4 writer interfaces
- add local helper tests
- verify compile and targeted tests

No further decomposition is required before writing the implementation plan.
