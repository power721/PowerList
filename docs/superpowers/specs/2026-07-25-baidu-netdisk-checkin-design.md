# Baidu Netdisk Check-in Design

## Goal

Add optional automatic daily membership check-in and daily-question answering to the existing `BaiduNetdisk` storage driver. The activity uses the Cookie already configured for that storage instance and runs without delaying or disrupting normal file operations.

## Scope

The feature will:

- Add an `AutoCheckin` storage option that defaults to disabled.
- Perform the Baidu membership daily check-in.
- Retrieve and answer the daily membership-growth question.
- Treat already-completed responses as successful idempotent outcomes.
- Run once asynchronously after successful driver initialization and then every 24 hours.
- Stop and replace background scheduling correctly during drop and reinitialization.
- Log concise results without exposing account credentials or activity content.

The feature will not split a Cookie into multiple accounts, add random delays, send notifications, query the account profile, log the daily question or answer, or provide a configurable cron expression. Each `BaiduNetdisk` storage instance represents one account.

## Components and Lifecycle

The implementation will add a focused `drivers/baidu_netdisk/checkin.go` module. It will contain the activity response types, private request helper, check-in workflow, daily-question workflow, safe message formatting, and scheduler lifecycle methods. Keeping this code separate avoids adding another responsibility to the existing `extension.go` file.

`Addition` gains an `AutoCheckin bool` field with JSON name `auto_checkin`. The option has no `default:"true"` tag, so existing and newly created storage instances remain opted out until explicitly enabled.

`BaiduNetdisk` gains a dedicated check-in scheduler. Initialization follows this lifecycle:

1. Stop and clear any scheduler left by an earlier initialization.
2. Complete the existing token, Cookie, and temporary-directory initialization.
3. Only after all existing initialization steps succeed, start automatic check-in when `AutoCheckin` is enabled.
4. Launch one best-effort check-in asynchronously.
5. Register the same job with a 24-hour interval scheduler.

`Drop` stops and clears the scheduler. Reinitialization never accumulates multiple schedulers. Activity errors are logged and do not change the result of a successful storage initialization.

## API Flow

Every run executes the check-in and daily-question activities independently and in order. A failed check-in does not prevent the question workflow from running.

### Membership Check-in

Send a GET request to:

`https://pan.baidu.com/rest/2.0/membership/level?app_id=250528&web=5&method=signin`

The request carries the storage Cookie and the minimal browser headers required by the endpoint. A response containing `points` is a successful new check-in. An `error_msg` containing `已签到`, `重复签到`, or `not allow` is an idempotent success. Other HTTP, decoding, or business failures are reported as activity errors.

### Daily Question

Send a GET request to:

`https://pan.baidu.com/act/v2/membergrowv2/getdailyquestion?app_id=250528&web=5`

Decode only the `answer` and `ask_id` fields required for the next request. If either field is absent, return an error for the question activity without attempting to submit an answer.

Submit the decoded values with a GET request to:

`https://pan.baidu.com/act/v2/membergrowv2/answerquestion?app_id=250528&web=5&ask_id=<ask_id>&answer=<answer>`

A response containing `score` is a successful answer. A `show_msg` containing `已回答`, `exceeded`, `超出`, or `超限` is an idempotent success. Other failures are reported as activity errors.

The implementation uses typed JSON response structs rather than regular expressions. Integral rewards, question IDs, and answers are represented in forms that accept the endpoint's JSON number encoding without logging their raw activity content.

## Request Isolation and Security

The activity endpoints use a small private request helper instead of the existing general-purpose `request` method. The existing method is designed for file APIs: it may append an access token, retry requests, and log complete responses. Those behaviors are unnecessary and unsafe for check-in requests.

The private helper uses the repository Resty client, sends the configured Cookie and fixed browser headers, and allows package-local endpoint and client seams for tests. Production defaults always target `https://pan.baidu.com` with the real shared HTTP client.

No Cookie, response header, complete response body, question, answer, request URL, or query value is logged. Transport errors are reduced to their error type, and HTTP errors include only the status code. Business messages are trimmed, have the complete Cookie and individual Cookie values replaced with `[redacted]`, and are bounded before being returned or logged.

## Results and Error Handling

The check-in and answer operations each return a small result containing whether the activity was newly completed or already complete and the awarded points when present. The background runner logs one concise line for each activity.

Malformed successful responses are errors rather than implicit successes. Missing check-in result fields, missing question identifiers, and missing answer result fields never panic. Since the workflow is best effort, failures are retried only on the next scheduled run or driver reinitialization; the feature adds no independent retry loop.

## Testing

Tests will be added under `drivers/baidu_netdisk` using local HTTP handlers and package-local seams. They will cover:

- Successful check-in and point parsing.
- Already-signed responses as idempotent success.
- Daily-question retrieval and correct answer submission.
- Already-answered and exhausted-limit responses as idempotent success.
- Continued question execution after a check-in failure.
- HTTP failures, malformed JSON, and missing required fields without panic.
- Cookie and individual Cookie-value redaction plus bounded business messages.
- Disabled automatic check-in creating no background work.
- Immediate asynchronous execution and a 24-hour schedule when enabled.
- Scheduler replacement on reinitialization and cleanup in `Drop`.

Focused verification will use `go test ./drivers/baidu_netdisk -count=1`. Repository-wide tests will also be attempted, with unrelated pre-existing or environment-dependent failures recorded separately.
