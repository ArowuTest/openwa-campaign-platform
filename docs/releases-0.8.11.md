# Release 0.8.11 — Audience Operational Core I

This release moves cohort and segmentation work from client-supplied snapshot membership toward authoritative server-side execution.

## Changes

- Added database-backed cohort estimation from the governed nested filter compiler.
- Added bounded server-side cohort member materialisation with deterministic eligibility evidence hashes.
- Added campaign entitlement enforcement during server-side snapshot materialisation.
- Added saved audience segment creation, listing, retrieval, optimistic update, archive and immutable version history.
- Added append-only PostgreSQL segment definition versions and active-name uniqueness per organisation.
- Added APIs for cohort estimates, saved segments and server-side audience snapshot materialisation.
- Reconciled OpenAPI coverage to all 114 implemented control-plane routes.
- Recorded the 25-screen designer HTML package as a future frontend reference with explicit engineering authority to redesign it around the actual backend and UI/UX requirements.

## Remaining scope

- Durable asynchronous cohort materialisation with progress, leases and restart-safe resume.
- Segment clone and semantic version comparison.
- Large-dataset PostgreSQL query-plan and performance evidence.
- Complete import conflict batching and operator completion workflow.
- Live PostgreSQL execution remains blocked until a reproducible database driver is linked.

- Cohort eligibility now resolves the latest effective consent grant and requires the organisation to remain active, matching final-dispatch precedence.
