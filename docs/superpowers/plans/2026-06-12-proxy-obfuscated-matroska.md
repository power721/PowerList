# Proxy Obfuscated Matroska Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the local proxy play files whose first eight bytes are a PNG signature and whose real Matroska EBML header starts at byte 8.

**Architecture:** Add a focused stream helper that detects the strict obfuscation signature and wraps a `model.RangeReaderIF` with an offset-mapping reader. Wire `server/common.Proxy` to convert URL links to range readers, apply the helper before `net.ServeHTTP`, and expose the corrected size and content type only when detection succeeds.

**Tech Stack:** Go, `model.RangeReaderIF`, `pkg/http_range`, `internal/net.ServeHTTP`, standard `net/http/httptest` tests.

---

### Task 1: Add Stream Detection and Offset Reader

**Files:**
- Create: `internal/stream/obfuscation.go`
- Create: `internal/stream/obfuscation_test.go`

- [x] **Step 1: Write failing stream tests**

- [x] **Step 2: Run tests to verify they fail**

- [x] **Step 3: Implement stream helper**

- [x] **Step 4: Run tests to verify they pass**

### Task 2: Wire Detection Into Proxy

**Files:**
- Modify: `server/common/proxy.go`
- Create: `server/common/proxy_test.go`

- [x] **Step 1: Write failing proxy tests**

- [x] **Step 2: Run tests to verify they fail**

- [x] **Step 3: Update proxy range path**

- [x] **Step 4: Run proxy tests**

### Task 3: Verify Focused Packages

**Files:**
- Verify: `internal/stream`
- Verify: `server/common`

- [ ] **Step 1: Run stream package tests**

Run: `go test ./internal/stream`

Expected: PASS.

- [ ] **Step 2: Run common package tests**

Run: `go test ./server/common`

Expected: PASS.

- [ ] **Step 3: Review diff**

Run: `git diff -- internal/stream server/common docs/superpowers`

Expected: Diff contains only the detection helper, tests, proxy wiring, and docs.

