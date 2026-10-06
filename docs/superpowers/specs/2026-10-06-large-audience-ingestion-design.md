# Large Audience Ingestion Capability Design

**Date:** 2026-10-06
**Status:** Implementation baseline for Day-1 closure
**Scope:** Large audience source upload, validation, reconciliation and approval
**Primary requirements:** UI-AUD-03..07, Prototype B, AC-002, IMP-001.., MET-002

## Capability

An authorised internal operator can ingest a multi-million-recipient CSV/XLSX source without keeping the browser open, without loading the source into browser or control-api memory, and without restarting from zero after an interrupted transfer or worker restart. The system durably records source lineage, protects MSISDNs, scans and validates asynchronously, exposes bounded progress and issues, reconciles totals, requires maker/checker approval, and preserves replay/audit evidence.

A two-million-MSISDN CSV is a normal large import, not a special emergency path. The architecture must remain viable for the UI/UX ten-million-profile operating model.

## Non-goals

- The browser does not parse, deduplicate, encrypt or materialise millions of rows.
- Upload completion is not import approval.
- The control API does not retain a whole large source in memory.
- The gateway fleet never receives audience source files.
- This tranche does not weaken consent, suppression, maker/checker, privacy or malware controls.
- This tranche does not require every S3-compatible provider to expose the same direct-upload mechanism; transport is abstracted behind the upload-session contract.

## Fixed invariants

1. **Bounded browser memory.** The browser reads at most one configured part plus small UI state at a time.
2. **Bounded API memory/disk.** Any relayed part is capped independently; the control API never spools the complete source.
3. **Durable resumability.** Uploaded parts and session state survive browser closure, reconnect, API restart and worker restart.
4. **Idempotent part identity.** A part number for a session may replay only with identical size/checksum; conflicting bytes fail closed.
5. **Private quarantine.** Source parts are inaccessible to ordinary users and are not processable until the complete manifest is verified.
6. **Server-authoritative verification.** Client progress is advisory; completion is based on storage metadata and the durable manifest.
7. **PII minimisation.** Raw MSISDNs never appear in upload-session API responses, URLs, logs, metrics or evidence.
8. **Protected processing.** Full validation streams rows; valid candidates cross the parser boundary only after encryption/HMAC and with raw E.164 cleared.
9. **Database-backed deduplication.** Full ingestion does not retain millions of identities in an application map.
10. **Fenced workers.** Session finalisation, validation and merge work use durable state/leases so stale workers cannot commit.
11. **Accounting.** Uploaded rows, valid, invalid, duplicate, suppressed, conflict, inserted, updated, unchanged, consent-ineligible, capped and final-eligible counts are either authoritative or explicitly marked unavailable; the UI never invents missing stages.
12. **Browser independence.** Once bytes are landed, scan/validation/reconciliation continue without an open browser.
13. **Retention.** Source parts/manifest are governed by the same retention/deletion evidence as existing import sources.
14. **Compatibility.** Existing small multipart intake remains temporarily supported for bounded callers until the resumable path is proven and migration is complete.

## Capacity envelope

Initial production envelope:

- logical source size: up to configured `MAX_IMPORT_FILE_BYTES`, never above the existing hard fail-closed ceiling;
- normal acceptance case: 2,000,000 CSV records;
- design target: 10,000,000 canonical-profile operating estate;
- upload part size: server-selected, default 8 MiB, configurable within a safe 5-32 MiB range;
- maximum parts: derived from configured maximum source size and part size, with an explicit hard cap;
- full CSV validation: streaming;
- staging write batch: existing bounded server batch, benchmarked rather than guessed;
- row-level issue retention/response: bounded; aggregate issue counts remain complete.

Limits are returned by the server and rendered by the UI. They are not hard-coded into frontend source.

## Upload-session state model

`CREATED -> UPLOADING -> UPLOADED -> FINALISING -> IMPORT_CREATED`

Terminal alternatives:

- `ABORTED`
- `EXPIRED`
- `FAILED`

Rules:

- `CREATED/UPLOADING`: parts may be uploaded/replayed.
- `UPLOADED`: every expected part is present and storage metadata matches the manifest.
- `FINALISING`: a leased worker is computing full-file evidence, inspecting actual format and malware-scanning.
- `IMPORT_CREATED`: the existing `audience_imports` workflow owns validation/approval/merge from this point.
- terminal sessions cannot accept new parts.
- expiry/abort deletes parts asynchronously; deletion is idempotent and audited.

## Storage shape

Each session owns immutable part keys under a non-guessable, server-derived quarantine prefix.

Example conceptual layout:

```text
imports/uploads/<opaque-session-hash>/
  part-000001.bin
  part-000002.bin
  ...
  manifest.json
```

The manifest contains only non-secret storage evidence:

- session ID;
- expected total byte size;
- configured part size;
- ordered part number/key;
- verified part byte size;
- verified SHA-256 per part;
- original filename;
- creation/finalisation timestamps;
- manifest version.

No raw recipient values appear in the manifest.

## Transfer adapters

The upload-session API is transport-neutral.

### Preferred S3-compatible path

When the configured object store supports governed presigned writes, the API returns a short-lived upload target for one part. The browser computes SHA-256 for that bounded part and uploads directly to the private object store. The server later verifies object size/checksum metadata before accepting the part, and the finaliser re-hashes the actual part bytes while reading the composite source before import creation.

Direct browser-to-object-store transfer is **disabled by default** and may be enabled only after the target bucket/service has a production CORS policy restricted to the approved admin-web origin(s), PUT/HEAD as required by the provider flow, and the exact signed request headers used by the direct target. Wildcard production origins are not acceptable. The rollout gate must also prove that no reusable object-store credential, presigned URL, or full recipient value is persisted in application logs, metrics, audit events or durable evidence. Until that gate is closed, the bounded relay transport remains authoritative.

### Bounded relay fallback

For environments without safe direct upload, the same session exposes a bounded part relay. One part is streamed through the control API to the existing ObjectStore with a strict per-part body limit. Only the part may touch temporary disk; the complete source never does.

Both adapters converge on identical durable part evidence and finalisation semantics, so the UI and worker state machine do not depend on the transport.

## API contract

Canonical OpenAPI remains authoritative.

### Create

`POST /api/v1/audience-import-upload-sessions`

Requires `audience.write` and an idempotency key.

Input includes governed import context and file envelope:

- organisation;
- approved/reviewable consent reference;
- purpose/channel/wording;
- source name/system;
- original filename;
- expected byte size;
- template/mapping/update policy.

Response:

- opaque session ID;
- state/version;
- part size/count;
- expiry;
- supported upload transport(s);
- no storage credentials.

### Read/resume

`GET /api/v1/audience-import-upload-sessions/{id}`

Returns current state, version, expected/uploaded bytes, part completion bitmap/list, expiry, and linked import ID when created.

### Acquire part target

`POST /api/v1/audience-import-upload-sessions/{id}/parts/{number}/target`

Returns a short-lived one-part upload target or relay descriptor. The target is bound to session, part number, expected maximum size and expiry.

### Relay part

`PUT /api/v1/audience-import-upload-sessions/{id}/parts/{number}`

Used only by relay transport. Requires per-part SHA-256 header and exact/bounded size. Exact replay converges; conflicting replay returns 409.

### Confirm direct part

`POST /api/v1/audience-import-upload-sessions/{id}/parts/{number}/confirm`

The server verifies storage metadata and records the immutable part result. It does not trust browser-reported success alone.

### Complete

`POST /api/v1/audience-import-upload-sessions/{id}/complete`

Requires optimistic version. Server verifies every part and total byte accounting before transition to `UPLOADED`; it does not scan/parse synchronously.

### Abort

`POST /api/v1/audience-import-upload-sessions/{id}/abort`

Records reason and schedules idempotent source cleanup. Cannot abort after an import has taken ownership without using the existing import cancellation/rollback semantics.

## Finalisation worker

A new bounded audience-worker component claims `UPLOADED` sessions with a lease/fence.

For each session:

1. Re-read and verify the authoritative part manifest.
2. Open an ordered composite source reader.
3. Compute whole-file SHA-256 while streaming.
4. Inspect actual format; browser MIME type is never authoritative.
5. Re-open the composite reader and malware-scan the full byte stream.
6. If XLSX requires random access, spool on the dedicated worker to a bounded temporary file, never on the control API.
7. Create/replay the existing immutable import batch with whole-file evidence and the governed mapping/context.
8. Mark the upload session `IMPORT_CREATED` with linked import ID.
9. Existing validation worker performs protected row processing/staging.
10. Any retry after response/process loss converges on the same session/import evidence.

A finaliser crash cannot create two imports because both upload-session request identity and existing import idempotency remain authoritative.

## Composite source abstraction

Introduce an import-source opener separate from generic `ObjectStore.Open`.

It supports:

- existing single-object source;
- ordered upload manifest source.

CSV consumers receive a streaming reader. XLSX receives a seekable worker-local bounded representation when necessary.

This keeps manifest complexity out of CSV normalisation/staging and allows future storage transport changes without altering campaign/audience domain logic.

## Database model

New forward-only migration after migration 0094:

### `audience_import_upload_sessions`

Stores:

- ID, organisation, actor and request identity;
- governed import metadata;
- original filename;
- expected total bytes;
- part size/count;
- upload transport;
- state/version;
- expiry;
- final whole-file hash/media type when known;
- linked import ID;
- finaliser lease owner/version/expiry;
- failure reason;
- timestamps.

Unique request identity prevents duplicate sessions for the same organisation/idempotency key.

### `audience_import_upload_parts`

Stores:

- session ID + part number primary key;
- derived immutable object key;
- expected byte range/size;
- verified byte size;
- verified SHA-256;
- state/timestamp.

Part rows contain no recipient data.

Indexes support:

- active sessions by expiry/state;
- finaliser claims by state/lease;
- part enumeration by session/number;
- source cleanup.

## UI/UX

The import experience is a resumable job workspace, not a single form.

### Step 1 — Source

- organisation, consent review, source and mapping context;
- file selection;
- server-provided size/format envelope before transfer;
- no browser parsing of millions of records.

### Step 2 — Upload

- uploaded bytes / total;
- parts complete / total;
- transfer rate and elapsed time;
- pause/resume/cancel;
- reconnect/resume after reload;
- explicit state if the selected local file no longer matches session size/name;
- browser computes only one bounded part checksum at a time.

### Step 3 — Security and validation

After upload completes the operator may leave.

UI shows durable stages:

- uploaded;
- malware scanning;
- validating;
- reconciling;
- awaiting approval;
- importing/merging;
- complete / complete with exceptions / failed.

No spinner implying the browser is doing the work.

### Step 4 — Reconciliation

Display server-authoritative counts, conflicts and bounded issue samples. Large error exports are background jobs.

### Step 5 — Approval

Maker/checker, MFA/step-up where required, immutable evidence, import version and reconciliation summary.

## Security

- upload IDs/object keys are opaque and scoped;
- permission + organisation scope checked at every session endpoint;
- no client-selected object key;
- no reusable object-store credentials;
- direct targets short-lived and one-part scoped;
- all sizes and hashes verified server-side;
- actual file signature checked after landing;
- full-source malware scan before validation;
- no raw MSISDN in logs, metrics, URLs or upload metadata;
- idempotency keys are bounded and never treated as authorisation;
- CSRF protections apply to browser state-changing API calls;
- abort/cleanup and expiry do not permit arbitrary object deletion;
- rate/concurrency limits apply per user/organisation and globally.

## Observability

Metrics are aggregate and PII-free:

- active upload sessions by state;
- bytes accepted;
- part retries/conflicts;
- upload duration histogram;
- finaliser backlog/age;
- scan duration/failure;
- validation rows/sec;
- staging batches/sec;
- merge duration;
- failed/expired cleanup backlog.

Structured logs use opaque session/import IDs only.

## Benchmark and acceptance matrix

### Transfer

- 2,000,000-row synthetic CSV succeeds through upload, resume, finalise, validate, reconcile and merge.
- interrupt at 25%, restart browser/session and resume without retransmitting completed parts.
- exact part replay is idempotent.
- conflicting part replay is rejected.
- API process restart during upload does not lose completed parts.
- finaliser crash after full hash but before import link converges safely on retry.

### Resource profile

Record:

- browser peak memory;
- control-api peak memory and temporary disk;
- audience-worker peak memory and temporary disk;
- PostgreSQL size/index growth;
- object-store bytes/object count;
- validation/staging rows per second;
- end-to-end wall time.

No acceptance based only on row-count constants.

### Correctness

Hard accounting block must reconcile source -> valid/invalid/duplicate -> staged -> conflict/suppressed/eligibility -> inserted/updated/unchanged -> final eligible.

### Scale extension

After the 2M composed gate is green, execute larger synthetic estate tests toward the 10M profile design target, including cohort/snapshot queries. Do not claim 10M acceptance from a 2M test.

## Migration and rollout

1. Add upload-session tables and API alongside current multipart endpoint.
2. Add finalisation worker and composite source opener.
3. Build new UI against upload-session contract.
4. Run 2M composed acceptance and failure injection.
5. Enable resumable path as default for admin-web.
6. Keep legacy multipart only for bounded compatibility callers.
7. Deprecate/remove legacy large-source use only after production evidence.

## Acceptance criteria

This capability is not accepted until:

1. control-api never needs to buffer/spool a complete large source;
2. browser disconnect/reload resumes from durable completed parts;
3. whole-file validation, malware scan and import creation are asynchronous and restart-safe;
4. full processing uses bounded memory and durable dedup/staging;
5. 2M composed acceptance passes with measured resource evidence;
6. accounting reconciles exactly or explicitly records bounded known categories;
7. privacy/security gates pass;
8. the deployed UI exposes progress/recovery/approval without fabricating state;
9. browser QA covers reconnect, failure and approval states;
10. evidence is frozen and independently reviewed before commit/deploy.
