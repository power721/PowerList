# Proxy Obfuscated Matroska Design

## Goal

Make the local download proxy expose a playable Matroska stream when an
upstream file has exactly one PNG signature prepended before its real EBML
header.

The supported byte layout is:

```text
offset 0:  89 50 4e 47 0d 0a 1a 0a
offset 8:  1a 45 df a3
```

This feature must not depend on ffmpeg, ffprobe, mpv, or any other external
media tool.

## Scope

- Apply only to requests served through the local proxy.
- Detect only the strict `PNG signature + EBML header at offset 8` pattern.
- Preserve ordinary files byte-for-byte.
- Preserve HTTP Range support, including seeking and suffix ranges.
- Expose the corrected representation with a size eight bytes smaller than
  the upstream object.
- Expose the corrected representation as `video/x-matroska`.

The feature will not scan arbitrary offsets, identify other media containers,
or modify direct-link redirects.

## Architecture

Add a small stream-layer helper that probes the beginning of a
`model.RangeReaderIF` and, when the supported signature is present, returns a
wrapper representing the corrected stream.

The wrapper maps every client-visible range to the upstream range:

```text
upstream start = client start + 8
length         = client length
visible size   = upstream size - 8
```

`server/common.Proxy` will obtain a range reader before serving content, call
the detection helper, and pass the resulting reader and visible size to
`internal/net.ServeHTTP`. When detection succeeds it will override the
response content type with `video/x-matroska`.

Detection must occur before response headers are written. A failed probe must
not break ordinary proxying; it falls back to the original reader and size.

## Detection

The detector reads only the first 12 bytes:

1. Bytes `0..7` must equal the PNG signature.
2. Bytes `8..11` must equal the EBML signature.

Both conditions are required. A normal PNG, a normal MKV, and any partial or
unreadable header remain unchanged.

## Proxy Behavior

For a matching upstream object of size `N`:

- A full request returns upstream bytes `8..N-1`.
- `Content-Length` is `N-8`.
- `Accept-Ranges` remains `bytes`.
- `Range: bytes=A-B` reads upstream bytes `A+8..B+8`.
- `Content-Range` is expressed using client-visible offsets and total `N-8`.
- `Content-Type` is `video/x-matroska`.

For a non-matching object, existing proxy behavior is unchanged.

## Error Handling

- If a source is smaller than 12 bytes, skip detection.
- If probing fails, log at debug level and preserve existing proxy behavior.
- If a translated range would exceed the upstream size, existing range-reader
  bounds handling remains responsible for the error.

## Testing

Add focused tests covering:

- strict signature detection
- rejection of ordinary PNG and ordinary MKV inputs
- translated full and partial range reads
- corrected visible size
- proxy response body, `Content-Length`, `Content-Range`, and Matroska content
  type for an obfuscated MKV
- unchanged proxy response for an ordinary file

