# Weixin channel

GoHermit implements Weixin as a first-class, owner-scoped channel. The
transport is an independent Go HTTP client inspired by the protocol documented
in Tencent/openclaw-weixin at commit
cef0bfc390393f716903e16d50408118047f87e0. No OpenClaw runtime, plugin SDK,
installation script, or source code is loaded at runtime.

The upstream project is MIT-licensed. GoHermit uses the public protocol shape
and records this fixed audit commit for attribution; it does not copy upstream
runtime or plugin code.

## Flow and state boundary

An inbound text message is keyed by account ID, channel, and peer ID. A
configured binding creates one queued Employee Task and sends a fixed
acknowledgement. The channel never prepares or starts a Task. Only the Owner
can Start it from the existing Task UI. Session, Run, Plan, Approval,
Verification, and recovery remain the existing control-plane truth; Weixin
events are not Session SSE and reconnect does not recover a Run.

The poller is one cancellable goroutine per account. It persists the upstream
getUpdates cursor only after bounded inbox/idempotency persistence. A duplicate
account/peer/message identity cannot create a second Task. Backoff is bounded
and cancellation-safe.

## QR login and secrets

Settings starts a bounded QR attempt. Status is server-derived and the QR
endpoint is same-origin, no-store, and never redirects to the upstream. Public
responses contain only masked account metadata and attempt status. Bearer
tokens, QR payloads, context tokens, and login secrets stay in a separate
0600 credentials file under the owner-scoped channel store. Logout invalidates
the stored secret and stops the account poller.

The store rejects traversal, symlink/non-regular files, invalid UTF-8, unknown
or oversized JSON, and IDs outside the ASCII path-safe allowlist. Account,
cursor, inbox, binding, login-attempt, and outbox records are bounded and
atomically replaced one file at a time.

## Transport contract

The client uses the upstream JSON endpoints get_bot_qrcode,
get_qrcode_status, getupdates, sendmessage, getconfig, sendtyping, and logout.
It sends the fixed bounded bot agent GoHermit/0.8-dev, validates HTTP origins,
disables redirects, enforces the production `ilinkai.weixin.qq.com` origin
allowlist (with loopback/reserved `.test` origins only for tests), caps bodies,
rejects invalid UTF-8, applies request timeouts, and maps non-success responses
to sanitized errors. Media is not
auto-imported; unsupported media receives a fixed bounded response.

The outbox records only fixed acknowledgements, safe final text, message kind,
attempt count, and delivery state. Delivery is stable and idempotent; unknown
delivery is not retried forever. Context tokens are scoped to their account and
peer and are not exposed by the API.

## API and UI

The same-origin API provides channel/account listing, QR login start/status/QR
and cancel, logout, binding CRUD, and bounded inbox listing. There is no public
arbitrary-send endpoint. The Settings WeChat connection card uses Ant Design
accounts, QR modal, status tags, binding form, and bounded inbox-oriented
metadata. It is responsive from 360px upward and contains an explicit warning
that inbound messages remain queued until Owner Start.

## Testing and limitations

Fake Weixin backends cover QR states, separate secret persistence, cursor and
context-token handling, idempotent inbound delivery, unbound fail-closed
routing, explicit Start, body/endpoint limits, redirect rejection, and API
secret redaction. Real WeChat scanning is manual Owner acceptance only and is
never automated or run with real credentials. Group policy, media expansion,
and external delivery remain deliberately bounded follow-up work.
