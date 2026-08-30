# I4 Fresh15 Remediation Design

## Purpose

Close the active I4 production-hardening boundary as one substantial engineering chunk in the authoritative local checkout. The chunk begins at the Fresh14 reviewed candidate plus its current remediation edits and ends only after deterministic verification, exact-source Fresh15 review, independent adjudication, and acceptance.

## Authoritative inputs

- The live worktree on `work/backend-production-engineering` is authoritative.
- Later user-approved conversation decisions override older repository prose where they conflict.
- Requirements, traceability, architecture decisions, and deterministic evidence outrank reviewer opinion.
- Fresh14 review output is diagnostic historical evidence after source changes; it is not acceptance authority.

## Scope

The chunk contains four related governance corrections:

1. A runtime heartbeat may reaffirm an existing governed node `InternalURL`, but may not rewrite it. A node without a governed URL may continue to establish its first URL under the existing bootstrap contract.
2. An authenticated rejection for a valid but unassigned node must retain immutable PostgreSQL audit evidence even when the node declares a nonexistent pool. Runtime events may omit a pool only for `REJECTED` events; accepted/heartbeat events remain pool-anchored.
3. Waiver maker/checker identifiers are compared canonically so trivial case variation cannot represent the same actor as two actors.
4. Governance tests must not expire because wall-clock time crossed a hard-coded fixture date. Production expiry behavior remains real-time and fail-closed.

No arbitrary maximum waiver horizon is introduced in this chunk. Earlier independent adjudication classified that proposal as policy hardening without an authoritative duration. The absence of a governed maximum remains explicit for later configuration-policy design rather than being silently invented here.

## Safety invariants

- Preserve sticky `UNKNOWN`; no automatic resend after ambiguous submission.
- Preserve node-addressed routing and sender/session lease and fence authority.
- Preserve the sibling transport model: Baileys, WWebJS, and direct Meta Cloud API.
- Path/node mismatch is rejected before nonce consumption.
- Authoritative pool-miss/drift rejection consumes the nonce and writes one immutable rejection record.
- Transient store failures neither consume the nonce nor create governance rejection evidence.
- No reset, clean, stash, rebase, amend, force operation, deployment, provider traffic, or production mutation.
- Stage and commit only after Fresh15 acceptance.

## Verification and review boundary

Focused RED/GREEN tests run continuously during implementation. The expensive council runs once on the complete coherent candidate after focused, governance, PostgreSQL, Go, race, TypeScript, security, topology, release, and evidence gates are green.

Every Fresh15 reviewer receives identical packet bytes and no other reviewer output. Findings are normalized and independently classified as `CONFIRMED`, `PARTIALLY_VALID`, `REJECTED`, or `INSUFFICIENT_EVIDENCE`. Valid defects require deterministic reproduction before remediation. Any reviewed source/config change invalidates Fresh15 and requires the next sequential exact-source review.

## Follow-on boundary

After accepted I4 is committed locally, the next substantial backend chunk is durable delivery reconstruction plus pause/resume/cancellation semantics. Campaign-scoped sender identity is retained as an approved later backend tranche: requested versus observed profile identity, verification before readiness, one active campaign identity per account, engine capability evidence, and capability-gated username operations.
