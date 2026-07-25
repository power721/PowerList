# Quark Check-in Design

## Goal

Add automatic daily capacity-growth check-in to the existing Quark storage driver, using the configured storage Cookie and the same immediate-plus-24-hour scheduling model as the 189CloudPC check-in.

## Scope

The change applies to the shared `drivers/quark_uc` implementation but runs only for storage instances whose registered driver name is `Quark`.

It will:

- Add an `AutoCheckin` storage option.
- Check the current Quark daily growth-sign state.
- Skip the sign request when the account has already signed in today.
- Submit the cyclic sign request when the account has not signed in.
- Log the daily capacity reward and consecutive-sign progress.
- Run once asynchronously after successful initialization and then every 24 hours.
- Stop the check-in scheduler when the storage is dropped.

It will not call the Quark account-profile endpoint, split multiple Cookies, send notifications, add random delays, or expose a cron-expression setting. UC storage instances will never call the Quark growth-sign endpoints.

## Configuration and Lifecycle

`Addition` gains an `AutoCheckin bool` field with JSON name `auto_checkin`. The option shares the Quark/UC addition schema, but runtime checks require both `AutoCheckin == true` and `d.config.Name == "Quark"`.

`QuarkOrUC` gains a dedicated `*cron.Cron` field for this activity. After the existing `/config` validation succeeds, initialization starts check-in without delaying the rest of storage setup:

1. Launch one background check-in immediately.
2. Create a 24-hour interval scheduler.
3. Register the same check-in function with the scheduler.

`Drop` stops and clears the scheduler when it exists. Reinitialization must stop any existing check-in scheduler before creating a replacement so repeated saves do not accumulate background jobs.

## API Flow

The check-in workflow uses the configured Quark Cookie and the following fixed query parameters:

- `pr=ucpro`
- `fr=pc`
- `uc_param_str=`

The workflow is:

1. GET `https://drive-m.quark.cn/1/clouddrive/capacity/growth/info`.
2. Decode `data.cap_sign`.
3. If `sign_daily` is true, log the existing `sign_daily_reward`, `sign_progress`, and `sign_target`, then stop.
4. If `sign_daily` is false, POST `https://drive-m.quark.cn/1/clouddrive/capacity/growth/sign` with JSON body `{"sign_cyclic":true}`.
5. Decode `data.sign_daily_reward` and log the reward with progress `sign_progress + 1` out of `sign_target`.

Rewards are returned in bytes and displayed as whole MiB, matching the reference script's integer conversion.

## Request Isolation and Security

The growth endpoints use a small private request helper rather than changing the driver's normal `conf.api`, because file operations use a different host. The helper uses the repository Resty client and sends only the required Cookie, content type, and query parameters.

Package-local endpoint/client seams may be used by tests to route traffic through `httptest`. Production defaults remain the real Quark growth endpoint and the shared configured Resty client.

No Cookie, response headers, or complete raw response bodies are logged. Transport errors are reported without embedding request URLs that could contain sensitive data. Business messages are trimmed and bounded before logging.

## Response and Error Handling

Response structs contain only the fields required by the workflow. A successful HTTP request is not sufficient by itself: missing `data`, missing `cap_sign`, or a non-zero API code is returned as an error.

Check-in runs are best effort background operations:

- Errors are logged with the storage ID and driver name.
- Errors do not panic or fail unrelated file operations after initialization.
- A failed information request does not attempt the sign POST because the current daily state is unknown.
- A failed sign POST is logged and retried only on the next scheduled run or storage reinitialization; this feature adds no independent retry loop.

## Testing

Tests will be added under `drivers/quark_uc` using local HTTP handlers and package-local seams. Coverage will include:

- Already-signed accounts do not send the sign POST and report the existing reward/progress.
- Unsigned accounts send `sign_cyclic=true` with the required Cookie and query parameters.
- Sign rewards and updated progress are parsed correctly.
- API failures and malformed responses return errors without panic.
- UC instances and disabled Quark instances do not start check-in.
- Enabling Quark check-in triggers the immediate background action and creates the 24-hour scheduler.
- `Drop` stops and clears the scheduler.
- Logged failures do not expose the configured Cookie or unbounded response content.

Focused verification uses `go test ./drivers/quark_uc -count=1`. Repository-wide tests will also be attempted, with pre-existing environment failures recorded separately.
