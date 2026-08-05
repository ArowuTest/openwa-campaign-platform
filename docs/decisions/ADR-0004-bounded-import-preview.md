# ADR-0004: Audience import previews are bounded and non-persistent

## Status

Accepted.

## Decision

CSV previews stream through the source file, maintain aggregate counts for every row, retain only a configurable sample of valid candidates and cap the number of returned row-level issues.

Preview responses never include raw or reversibly encrypted MSISDNs.

## Rationale

A ten-million-row source file must not cause the API to retain ten million candidate objects or validation errors in memory. Full ingestion will be implemented as a durable asynchronous job after preview approval.
