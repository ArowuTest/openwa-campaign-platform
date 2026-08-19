# Meta Cloud Provider Design

**Date:** 2026-08-11
**Status:** Approved for implementation

## Goal
Add Meta's official WhatsApp Cloud API as a first-class third WhatsApp transport alongside OpenWA Baileys and OpenWA WhatsApp-Web.js, while preserving the existing campaign governance, delivery ledger, idempotency, UNKNOWN/no-resend, audit, privacy, capacity and execution guarantees.

## Approved scope
- Admin-configured Meta WABAs and phone-number senders; no customer Embedded Signup in this phase.
- Campaigns may select META_CLOUD, BAILEYS, WHATSAPP_WEB_JS, any pair, or all three.
- A selected set is an eligible distribution pool, never duplicate delivery of one recipient through multiple transports.
- Routing supports `AUTO` and `WEIGHTED`; AUTO is the default.
- WEIGHTED percentages are targets. If a route becomes unavailable before assignment, remaining work may rebalance to other selected healthy routes.
- Once a recipient reaches `SUBMITTING`, its transport is authoritative. UNKNOWN is never automatically resent through another transport.
- Meta supports approved-template delivery for business-initiated traffic, policy-permitted free-form delivery when explicit per-recipient eligibility exists, text/image/video/document campaign material, inbound messages, opt-out processing, and sent/delivered/read/failed status webhooks.
- Meta secrets are never stored as plaintext in PostgreSQL.

## Non-goals
- Meta Embedded Signup / customer self-service WABA connection.
- Automatic retry of an ambiguous Meta submission through Baileys or WWebJS.
- Changes to the existing OpenWA gateway protocol or session authority semantics.
- Frontend work, deployment, commit, push or live Meta credential provisioning in this phase.
## Architecture
The campaign backend remains authoritative. `OPENWA/BAILEYS` and `OPENWA/WHATSAPP_WEB_JS` continue through the signed OpenWA gateway. `META/CLOUD_API` is dispatched directly by the campaign worker through a dedicated Meta adapter using the Graph API.

The existing `dispatch.Gateway` boundary becomes a transport router. OpenWA requests retain all current gateway-session fencing evidence. Meta requests carry a governed Meta sender reference instead of a gateway node/session authority bundle.

Sender pools remain the common unit for campaign capacity reservation and shard distribution. A Meta sender belongs to one sender pool, allowing the current multi-pool routing, pacing, reservations, execution reporting and shard assignment model to be reused.

## Message-authority invariant
The approved provider-neutral OpenWA message version is the authoritative business content. Baileys and WhatsApp-Web.js must continue to support generic free-form personalised messages, including dynamic recipient variables and the message/media types supported by those transports. Meta-specific template rules are transport-adapter constraints only and must never narrow the canonical OpenWA message model or composer.

Provider compatibility is evaluated per route. An OpenWA route renders the canonical message directly. A Meta route renders either a compatible approved Meta template binding for business-initiated/template-required traffic, or a Meta free-form representation only where current Meta policy and conversation-window state explicitly permit it. Lack of a valid Meta representation may exclude or block the Meta route; it must not make the same canonical message invalid for Baileys or WhatsApp-Web.js.

## Governed Meta sender model
Create `meta_cloud_senders` with:
- internal ID, organisation ID and sender-pool ID;
- WABA ID, Meta phone-number ID, display name and business phone display value;
- credential key referencing runtime secret material;
- configurable Graph API version;
- DRAFT / PENDING_APPROVAL / ACTIVE / REJECTED / RETIRED lifecycle;
- independent maker/submitter/checker attribution and reason;
- HEALTH_UNKNOWN / HEALTHY / DEGRADED / UNAVAILABLE observation state and timestamp;
- immutable version and effective period evidence.

The database stores only the credential key. Runtime credentials are resolved from a read-only secret file keyed by credential key. A credential record contains the access token, app secret and webhook verify token. Missing credentials make Meta operations unavailable; they are never silently substituted.
## Template model
Create a local `meta_cloud_templates` catalogue synchronised from each active WABA. Store Meta template ID, name, language, category, status, quality signal when supplied, components JSON, a canonical component hash and the last successful sync time.

Create an optional immutable Meta binding for an approved message version. The binding records the Meta template name/language and canonical component hash plus typed parameter mappings for the template components that require per-recipient values. The binding is provider-specific representation evidence and never replaces the authoritative OpenWA message version. At routing approval, every selected Meta sender must have a matching approved template with compatible components.

The Meta renderer is component-driven rather than hard-coded around one template shape. The first implementation may support body variables and text/image/video/document headers, but the binding/rendering contract must be extensible to additional Meta component types such as location and dynamic buttons without changing the canonical campaign/message model.

For business-initiated bulk Meta traffic where Meta requires an approved template, a compatible Meta binding is mandatory. Meta free-form delivery may be used only when current Meta policy and per-recipient conversation-window evidence explicitly permit it; the platform must never assume that window for bulk traffic. Baileys/WWebJS continue to render the canonical approved message directly and remain free-form/personalised independently of Meta template availability.

## Routing and distribution
Add `distributionMode` to routing plans: `AUTO` or `WEIGHTED`.

Each route retains its sender-pool ID, provider, engine, capability definition, allocation weight and reserved capacity. OpenWA routes reference a governed gateway pool. Meta routes reference a governed Meta sender. Exactly one provider-specific endpoint reference is allowed per route.

For `AUTO`, the backend derives normalized route weights from healthy reserved throughput at plan approval. For `WEIGHTED`, operator-supplied positive weights are normalized as target proportions. Route weights are frozen into the approved routing plan for deterministic shard assignment.

Pre-assignment route loss may trigger governed shard reallocation only to another selected route that explicitly allows reallocation and satisfies provider/message capabilities. No recipient in `SUBMITTING`, `GATEWAY_ACCEPTED`, `SENT`, `DELIVERED`, `READ`, `UNKNOWN`, permanent failure, suppression or cancellation is cross-routed.
## Dispatch semantics
A transport-neutral material envelope carries recipient, message and route evidence. OpenWA material must continue to satisfy the existing gateway pool, node, session, lease and authority validation. Meta material must satisfy active Meta-sender governance, sender-pool membership, frozen provider-capability evidence, template compatibility and credential availability.

Meta outbound submission uses `POST /{phone-number-id}/messages` with Bearer authentication and `messaging_product=whatsapp`. The adapter supports template body parameters and optional image/video/document media header links. Graph API version is sender-configured, not compiled as a permanently fixed global constant.

HTTP/network failure classification follows the existing delivery safety model:
- request rejected before any network submission: permanent or safe-to-retry as appropriate;
- 429 rate-limit responses: safe-to-retry with Retry-After when supplied;
- transport timeout, unreadable success response, 5xx where provider acceptance cannot be excluded, or cancellation after request transmission: outcome UNKNOWN;
- a successful Meta response without a message ID: UNKNOWN.

UNKNOWN remains terminal for automatic dispatch and requires reconciliation.

## Webhooks and inbound
Expose Meta GET verification and POST event endpoints scoped by Meta sender ID. GET compares the supplied verify token through the runtime credential resolver and returns the challenge only on a valid subscription request.

POST bodies are size-bounded and authenticated using Meta `X-Hub-Signature-256` HMAC-SHA256 with the configured app secret before JSON parsing. Status notifications map `sent`, `delivered`, `read` and `failed` into the existing delivery ledger using the Meta message ID. Deduplication keys are deterministic over sender, message ID, status and provider timestamp so retries are replay-safe.

Inbound text messages are forwarded to the existing opt-out/inbound pipeline. Reply context provider-message IDs are retained so STOP replies can resolve the originating recipient without inventing a browser session. Unsupported inbound message types are recorded/acknowledged without bypassing signature validation.
## Capacity and health
Meta sender capacity is governed by its sender pool plus current Meta-sender health, not by fabricated gateway sessions. An ACTIVE Meta sender with current HEALTHY/DEGRADED evidence contributes sender-pool capacity; UNAVAILABLE or stale/unknown evidence contributes zero until verified.

Successful account verification/template sync and successful sends refresh health. Authentication/permission errors mark the sender unavailable; throttling marks it degraded; ambiguous network failures do not falsely mark a send failed and do not create a retry through another transport.

## API and governance
Admin APIs provide list/get/create/submit/approve-or-reject/retire/verify for Meta senders plus template sync/list. Maker-checker rules mirror existing provider/gateway governance. Sensitive responses never return access tokens, app secrets or verify tokens.

Routing-plan APIs expose distribution mode and provider-specific endpoint references. Existing OpenWA request shapes remain backward compatible where practical; missing `distributionMode` resolves to `WEIGHTED` for legacy explicit route weights, while new campaign tooling should default to AUTO.

## Security
- No Meta credential value in PostgreSQL, logs, audit evidence, OpenAPI examples or API responses.
- Secret files are read-only and resolved through the existing `_FILE` environment pattern.
- HTTP clients reject redirects so Bearer credentials cannot be forwarded to another host.
- Meta Graph base host defaults to `graph.facebook.com`; tests may inject a local server explicitly.
- Request/response bodies are bounded; error text is redacted and capped.
- Webhook signature verification occurs on the raw body before parsing.

## Verification gate
Implementation is complete only when deterministic unit tests, fresh PostgreSQL migration tests, focused Meta integration tests, the complete first-party Go suite, `go vet`, `go build`, OpenAPI route verification and existing security checks are green. Go tests run only in Docker. SQL changes are validated on fresh Docker PostgreSQL and disposable Neon PostgreSQL. Existing 13-service active stack is not modified.

## External acceptance gate
Live Meta credential/WABA verification, real template sync, real outbound sends and real Meta webhooks remain deployment acceptance evidence. Local implementation must not be represented as proving those external conditions.

## Official API basis
The adapter follows Meta's official WhatsApp Business Platform Cloud API contract: messages are sent through `/{Phone-Number-ID}/messages`; message status is delivered through WABA webhooks; templates are listed under `/{WABA-ID}/message_templates`; media links/uploads are supported by the Cloud API. API version remains configurable so a later Meta version change does not require an architectural rewrite.