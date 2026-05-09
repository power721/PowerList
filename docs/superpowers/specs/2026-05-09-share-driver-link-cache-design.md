# Share Driver Link Cache Design

## Summary

Add local in-process link caching for the following share drivers:

- `QuarkUCShare`
- `Cloud189Share`
- `BaiduShare2`
- `ThunderShare`
- `AliyundriveShare2Open`

The goal is to avoid repeating expensive share-to-personal-drive save or relay work when the same shared file link is requested repeatedly in a short time window.

For four drivers, the cache key is only the shared file ID. `AliyundriveShare2Open` is the one exception: it must cache separately for `AliTo115=false` and `AliTo115=true`.

## Goals

- Cache successful share-driver `Link` results for `60m`.
- Reduce repeated upstream save or relay requests for the same shared file.
- Keep the existing link-generation logic unchanged on cache miss.
- Keep all caching local to the target share drivers.
- Separate `AliyundriveShare2Open` cache entries by `AliTo115` state.

## Non-Goals

- No change to global `op.Link` caching behavior.
- No new user-facing config for TTL or key behavior.
- No coordination with temp-file cleanup delays.
- No attempt to guarantee cached links remain valid for the full `60m`.
- No cross-driver shared cache abstraction beyond what is needed for this change.

## Drivers in Scope

### `QuarkUCShare`

Current `Link` flow retries `d.link(...)`, which may save the shared file into a backing account and then request the final direct link.

Add a local `fileID -> *model.Link` cache in `drivers/quark_uc_share/driver.go`.

### `Cloud189Share`

Current `Link` flow retries `d.link(...)`, which may try share-play direct access first and then fall back to transfer-and-link behavior.

Add a local `fileID -> *model.Link` cache in `drivers/189_share/driver.go`.

### `BaiduShare2`

Current `Link` flow retries `d.link(...)`, which saves the shared file into the backing account, schedules deletion, and then calls the backing driver's `Link`.

Add a local `fileID -> *model.Link` cache in `drivers/baidu_share2/driver.go`.

### `ThunderShare`

Current `Link` flow retries `d.link(...)`, which saves the shared file and then resolves the final download URL.

Add a local `fileID -> *model.Link` cache in `drivers/thunder_share/driver.go`.

### `AliyundriveShare2Open`

Current `Link` flow may return either:

- the direct Aliyun open link
- or a 115-based result when `AliTo115` is enabled and the file is not excluded from 115 relay

Add a local cache in `drivers/aliyundrive_share2_open/driver.go`, but key it by:

- `file.GetID() + "|" + strconv.FormatBool(setting.GetBool(conf.AliTo115))`

This keeps the Aliyun-native result and the 115-relay result isolated from each other.

## Architecture

### Cache placement

Each driver should own its own package-local cache instance. Do not route this through `internal/op` or alter `op.Link` cache keys.

Recommended shape:

```go
var shareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
```

The concrete variable name can be driver-specific to avoid confusion when reading tests and logs.

### Lookup flow

For each target driver's public `Link` method:

1. Build the local cache key.
2. Check the local share-link cache.
3. If cached, return the cached `*model.Link` immediately.
4. If not cached, run the existing resolver path.
5. Only if `err == nil` and `link != nil`, store the final link into the local cache.
6. Return the resolver result.

This keeps retry loops, validation, and current share-save behavior intact on cache miss.

### Resolver seams

Add a small package-local resolver seam per driver so tests can verify caching behavior without calling real upstream APIs.

Recommended shape:

```go
var resolveShareLink = func(ctx context.Context, d *DriverType, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	return d.link(ctx, file, args)
}
```

For `AliyundriveShare2Open`, the seam should wrap the full public `Link` resolution behavior after the cache check, so the cached value represents the final returned link, not only the intermediate Aliyun result.

## Key Semantics

### Shared rule for four drivers

The cache key is only:

- `file.GetID()`

Applies to:

- `QuarkUCShare`
- `Cloud189Share`
- `BaiduShare2`
- `ThunderShare`

`LinkArgs` do not affect the cache key.

### Special rule for `AliyundriveShare2Open`

The cache key is:

- `file.GetID() + "|" + strconv.FormatBool(setting.GetBool(conf.AliTo115))`

This means:

- `AliTo115=false` has its own cache entries
- `AliTo115=true` has its own cache entries
- toggling the setting does not reuse the wrong final link type

## Cache Lifetime

- TTL is fixed at `60m`.
- TTL does not depend on the returned link's own `Expiration`.
- TTL does not reset temp-file deletion timers.
- A cached link may become stale before `60m`; this is accepted by design for this feature.

## Error Handling

- If the underlying resolver returns an error, do not write cache.
- If the underlying resolver returns `nil, nil`, do not write cache.
- Cache lookup itself should not introduce new user-visible errors.
- Cache misses should behave exactly like the current code.

## Testing

Add focused unit tests for each driver in package-local `driver_test.go` files.

### Required test cases for all five drivers

- same cache key requested twice returns the same cached link and only invokes the resolver once
- resolver error does not populate cache, so the next request invokes the resolver again
- different cache keys do not share entries

### Additional test case for `AliyundriveShare2Open`

- same `fileID` with `AliTo115=false` and `AliTo115=true` must resolve separately and populate separate cache entries
- repeating each state-specific request should then hit its own cached entry

### Test strategy

- Use resolver seams rather than real API calls
- Clear or replace the package-local cache between tests to avoid cross-test leakage
- Assert on resolver call counts and returned link URLs

## Risks

- Cached links may outlive the backing temp file or upstream link validity and therefore fail before the local cache TTL expires.
- Because `LinkArgs` are intentionally ignored, callers with different arg combinations will still share the same cached result.
- Per-driver duplicated cache glue is slightly repetitive, but the isolation keeps the change low-risk and easy to reason about.
