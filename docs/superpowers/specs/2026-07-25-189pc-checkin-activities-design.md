# 189CloudPC Check-in Activities Design

## Goal

Extend the existing `189CloudPC` automatic check-in so one scheduled run performs the current cloud check-in and prize draw, claims the VIP monthly 2 TB space benefit, and completes the green-energy activity check-in and all available task categories.

## Scope

The change is limited to the `drivers/189pc` check-in workflow. It will:

- Preserve the existing `/mkt/userSign.action` check-in.
- Preserve the existing `TASK_SIGNIN` prize draw.
- Claim the `ACT2025VIP2T` monthly VIP space benefit.
- Establish the merged SSO session required by the green-energy activity.
- Perform the `ACT2024cztx` daily green-energy check-in.
- Query and execute incomplete green-energy tasks of types `1`, `2`, and `3`.
- Query and log the current green-energy score.

The change will not add storage settings, expose activity identifiers as user configuration, send notifications, or reproduce the reference script's standalone access-token login flow. The driver already has the required session and access-token values after its normal login.

## Architecture

The existing `checkin` method remains the top-level best-effort coordinator. Activity-specific behavior is split into small private helpers in `drivers/189pc/extension.go`:

- Existing cloud check-in.
- Existing sign-in prize draw.
- VIP space claim.
- Green-activity SSO session creation.
- Green daily check-in and weekly-day lookup.
- Green task-list retrieval and task execution.
- Green score lookup.

The green workflow uses a dedicated Resty client with a cookie jar. This keeps the cookies created by `ssoLoginMerge.action` for subsequent green-activity calls without mutating the driver's shared file-operation client.

Small package-level request seams may be introduced where needed so tests can route activity traffic through local HTTP handlers. They must default to the production endpoints and remain private to the package.

## Data Flow

When `AutoCheckin` is enabled, the existing immediate background run and 24-hour cron schedule remain unchanged. Each run follows this order:

1. Call the standard cloud check-in endpoint with the driver's signed API request helper.
2. Call the existing `TASK_SIGNIN` prize endpoint.
3. Call `drawTargetSpace.action` with activity `ACT2025VIP2T` and prize `2T_2025VIP`.
4. Read `SessionKey`, `FamilySessionKey`, and `AccessToken` from the active driver token information.
5. Call `ssoLoginMerge.action` through the dedicated green client so its cookies are retained.
6. Call `signInNew.action`, then query `signInNewInfo.action` when the sign-in succeeds or the response indicates an already completed state that still permits the lookup.
7. For task types `1`, `2`, and `3`, call `getGreenTaskList.action`, select entries whose `status` is false, and submit each to `doGreenTask.action`.
8. Call `getGreenLevelList.action` and log the current `userScore`.

No access token, session key, session secret, or Cookie header is written to logs.

## Response Handling

Responses are decoded into narrow Go structs containing only fields used by the workflow. Business success is determined from the endpoint's documented fields rather than from HTTP transport success alone.

- VIP claim: log the returned message; code `0` is success. An already-claimed response is informational rather than fatal to the overall run.
- Green check-in: a true `result` is success. A false result or malformed response is logged with the server message or a bounded response description.
- Task list: only incomplete tasks are submitted. Empty or fully completed lists require no action.
- Task execution: a true `data` value is logged as completed; false is logged as skipped or rejected without aborting other tasks.
- Green score: log `userScore` when present; missing data is treated as an endpoint failure.

## Error Isolation

The coordinator is best effort. A failure in the ordinary check-in, prize draw, or VIP claim does not prevent later independent activities from running. Green substeps after SSO depend on the dedicated authenticated session: if SSO setup fails, the green workflow stops and reports that failure, while the rest of the check-in run remains complete.

Within the green workflow:

- Failure of the daily sign-in does not prevent task processing or the score query.
- Failure of one task category does not prevent the other categories.
- Failure of one task submission does not prevent remaining tasks.
- Failure of the final score query is logged without converting the cron callback into a panic.

Raw successful response bodies are not logged. Error logging must be useful while avoiding credentials and unbounded server payloads.

## Testing

Tests in `drivers/189pc/extension_test.go` will use local HTTP handlers or package seams to verify observable behavior. Coverage will include:

- VIP request activity, prize, session, and cache-busting parameters.
- Green SSO request parameters and retention of its response cookie.
- Green daily sign-in and weekly-day lookup.
- Filtering completed tasks and submitting incomplete tasks for all three task types.
- Continuing with later categories and score lookup after an earlier green endpoint fails.
- Stopping green substeps when SSO cannot establish the activity session.
- Ensuring logs and request construction do not expose token values beyond the required outbound parameters.

The focused package tests will be run first, followed by the repository-appropriate broader Go test command if the focused tests pass.
