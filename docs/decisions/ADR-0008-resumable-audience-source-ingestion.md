# ADR-0008: Resumable manifest-based audience source ingestion

## Status

Accepted for implementation.

## Context

ADR-0004 made audience previews bounded and explicitly deferred full ingestion to a durable asynchronous path. The production platform must accept multi-million-recipient sources while remaining safe under browser disconnects, control-api restarts, worker restarts and object-store retries.

The existing full parser is streaming and database-backed, but the current HTTP intake still carries one complete multipart source through the control API and the S3 adapter spools the complete object to local temporary disk before upload. Making the Day-1 UI depend on that path would turn a presentation gap into a long-term scale bottleneck.

## Decision

Introduce a durable upload-session contract backed by immutable bounded source parts and a manifest.

The upload session is transport-neutral:

- S3-compatible environments may upload a bounded part directly through a short-lived scoped target;
- a bounded relay may be used where direct upload is unavailable.

Direct S3 transfer is disabled by default. It may be enabled only after the production object store has an explicit CORS policy for the approved admin-web origin(s) and the exact signed headers/methods required by the one-part capability. Wildcard production origins and durable storage of presigned URLs are prohibited. The finaliser verifies actual part bytes against the durable manifest before import creation, so object metadata is never sufficient evidence on its own.

Every part has a server-derived object key, expected range/size and verified SHA-256. A complete session is finalised asynchronously by the audience worker, which streams the ordered parts, computes whole-file evidence, verifies actual file type, malware-scans the complete source and creates the existing governed audience-import batch.

The browser does not process recipient rows. The control API never buffers or spools the complete source.

## Consequences

### Positive

- browser/network interruptions resume by part;
- API memory and temporary-disk usage are independent of total source size;
- storage transport can evolve without changing the operator workflow;
- existing protected streaming validation/staging remains reusable;
- part replay is naturally idempotent and conflicts fail closed;
- whole-file scan/inspection occurs outside request latency;
- source lineage and cleanup remain auditable.

### Costs

- new upload-session/part persistence;
- a finalisation worker and composite-source abstraction;
- cleanup of expired/aborted part manifests;
- direct S3 target signing is an adapter concern and requires provider-specific verification;
- XLSX still needs bounded worker-local random access because ZIP parsing requires `ReaderAt`.

## Rejected alternatives

### Increase the existing multipart body limit

Rejected. It keeps the whole transfer coupled to one browser/API request and retains complete-file temp-spooling in the control plane.

### Parse in the browser

Rejected. It exposes PII handling to the client, scales poorly and makes correctness dependent on a browser staying open.

### Store millions of contacts in one request payload

Rejected. It violates bounded-memory, retry and privacy constraints.

### Create an import batch before bytes are durably complete

Rejected. Upload and import lifecycle have different truth. A partially uploaded object must not appear to be a valid import source.

### Require S3 multipart semantics in the domain

Rejected. The product contract should survive S3/MinIO/filesystem changes. Direct multipart/presigned transfer is an adapter optimization, not the authoritative business state.

## Compatibility

The existing single-object multipart intake remains temporarily available for bounded compatibility callers. The admin-web large-source journey moves to upload sessions after composed acceptance.

## Supersedes

This ADR does not supersede ADR-0004. It implements ADR-0004's deferred durable full-ingestion path.
