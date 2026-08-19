# 17 August 2026 — Authoritative Continuation Addendum

**This addendum supersedes status, migration-count, review-round and next-step statements in the older 13-August handover below. Historical sections remain for provenance.**

Current repo: `C:\Users\sanus\OpenWA\campaign-platform-active\repo`; branch `work/backend-production-engineering`; committed HEAD remains `786451d5616f5e179903fd9b61c5e26f6de76f77`. The worktree is intentionally very dirty and contains cumulative Task-6/Meta/final-hardening work. Never reset, clean, stash, split or discard it. No commit, push, deploy or frontend deployment is authorised.

The authoritative transport model is three sibling paths behind common orchestration: **OpenWA/Baileys, OpenWA/WWebJS/Chromium, and direct Meta Cloud API**. Meta FREE_FORM is permitted only from durable authenticated inbound-text evidence scoped to the exact Meta sender + contact conversation window; otherwise the governed Meta template path applies. Ambiguous provider outcomes remain sticky UNKNOWN and cannot be automatically resent through another provider.

Database migrations now run through **0089**. 0088 added the fail-closed TRUNCATE fence on live campaign capacity reservations; 0089 extends the same authority boundary to DELETE of HELD/ACTIVE reservations while retaining RELEASED tombstones and adds a migration-unique readiness capability. Fresh PostgreSQL 17 and PostgreSQL 18 verifiers replayed migrations 0001→0089 from zero and the targeted R17 PostgreSQL regressions passed on both.

R14 closed webhook/routing/authority defects; R15 closed gateway lifecycle/rate/body-binding defects; R16 closed Meta batch-isolation and lifecycle-command target-MAC defects; R17 closed five Meta/routing authority defects. R18 then produced a split verdict on routing-capacity surface B. Independent adjudication confirmed two source defects: sequential same-day windows could exceed a pool's independent daily entitlement, and terminal campaigns could mint new HELD capacity. The hourly portion of the first reviewer claim was rejected as over-broad. Post-R18 regressions now prove per-UTC-day daily reservation authority and terminal-state creation fail-close at both service and PostgreSQL boundaries.

Current R19 local backend source-freeze gates are GREEN: the accepted R18 regressions pass on PostgreSQL 17 and 18; complete execution unit/integration and race verification is green; fresh isolated complete Go tests/vet/build is green with marker R19_EXACT_GO_GREEN; production Compose/OpenAPI/secret/Node-security checks are green; gateway/admin npm audits report zero vulnerabilities. Generated traceability remains exact at **398 = 168 implemented/tested + 181 partial + 32 not started + 17 blocked external**.

R18 is preserved as remediation evidence. Fresh blind R19 reviewed the exact post-R18 candidate (tracked diff `77e51bf5e15e41ee7cc398cd02d6e9f0b749c0c844e61743528495baed0ecad8`, untracked content `7bbf7b68d621a57fffd637a8507fc906e29cc1ed0283f7e369cec0ee6080385b`). A/C/D each produced independent Grok+Qwen PASS evidence; B produced Qwen+Gemini PASS plus one Grok timezone-bucket claim. That claim was independently REJECTED because the production platform libpq driver forces every physical PostgreSQL session to UTC and no later timezone mutation exists. No R19 source defect survives adjudication, so the **local backend source freeze is established at R19**. The immediate engineering boundary is infrastructure/platform proof before frontend. Live Railway/Hostinger changes and real-provider sessions remain separate user-authorised actions.

External/live gates remain genuinely open: real Baileys/WWebJS/Meta sessions and acknowledgements, production-like capacity/endurance, Railway/Hostinger target-host networking and secret provisioning, independent security assurance, backup/PITR/DR, operational ownership, and the production frontend/UAT/accessibility phase. Do not relabel these as backend source failures and do not relabel the R19 local source freeze as production acceptance.

---



The earlier handover I drafted was useful as a checkpoint, but it was **not sufficiently precise in a few areas**, particularly the transport diagram. The version below corrects that and distinguishes four different states throughout:

* **IMPLEMENTED** — code exists in the current worktree.
* **VERIFIED** — we obtained concrete test/database evidence.
* **NEEDS FRESH VERIFICATION** — it passed previously, but later changes mean it must be rerun before final completion.
* **EXTERNAL / NOT YET ACCEPTED** — requires real WhatsApp/Meta/infrastructure/UAT evidence.

Below is the version I recommend saving as the new authoritative `PROJECT_STATUS_HANDOVER.md`.

---

# OpenWA Campaign Platform

# Backend Production Engineering + Meta Cloud API Integration

# Full Project Status & Continuation Handover

**Handover date:** 13 August 2026
**Workstream:** Backend Production Engineering / Task 6 / Meta Cloud API Extension
**Status:** Advanced implementation; final post-hardening verification in progress
**Do not interpret this document as a production acceptance declaration**

---

# 1. Purpose of this handover

This document is intended to allow a new engineering session or AI coding agent to continue the OpenWA backend work without restarting, redesigning, or accidentally undoing completed engineering.

It consolidates the latest known state of:

* the original OpenWA backend;
* Task 6 production-engineering work;
* Baileys transport;
* Chromium/WWebJS transport;
* the newly added official Meta WhatsApp Cloud API transport;
* campaign/provider-routing architecture;
* canonical message architecture;
* Meta sender governance;
* Meta templates and bindings;
* Meta dispatch;
* Meta webhooks and inbound processing;
* PostgreSQL migrations through 0077;
* concurrency and durability hardening;
* the four-instance Grok 4.6 adversarial review council;
* PostgreSQL 17 verification;
* Neon/PostgreSQL 18 compatibility evidence;
* Docker validation and cleanup;
* security/OpenAPI evidence;
* reviewer calibration;
* remaining final verification;
* remaining external/live production acceptance.

The actual repository and database migrations remain the ultimate source of truth.

A continuation session must inspect them before making further changes.

---

# 2. Current repository and working environment

## Active repository

```text
C:\Users\sanus\OpenWA\campaign-platform-active\repo
```

## Branch

```text
work/backend-production-engineering
```

## Earlier recorded HEAD checkpoint

```text
786451d5616f5e179903fd9b61c5e26f6de76f77
```

Relevant earlier commits include:

```text
786451d docs: record Task 5 checkpoint
1ad6376 feat: complete Task 5 requirements reconciliation
c728cfa feat: close Task 4 pagination and gateway telemetry
```

There is significant later Task 6 and Meta work in the **dirty worktree**.

That dirty state is intentional.

---

# 3. Critical repository safety constraints

The continuation agent must **not** attempt to “clean up” or normalise the Git worktree.

Do not execute destructive operations such as:

```text
git reset
git clean
git stash
```

Do not arbitrarily restore files from HEAD.

Do not split or rebuild the worktree.

Do not perform a destructive rebase.

Do not discard files merely because they are untracked or modified without first understanding their role.

The current worktree contains cumulative backend production-engineering and Meta integration work.

---

# 4. Commit / deployment boundary

No further action should:

* commit;
* push;
* merge;
* deploy;
* alter production;
* deploy frontend;
* mutate live infrastructure;

without separate user authorisation.

The immediate work remains:

**verification → review → documentation → local backend checkpoint**

not deployment.

---

# 5. Testing methodology

Strict defect workflow remains mandatory:

```text
Observed defect
      |
      v
Reproduce
      |
      v
RED regression test
      |
      v
Minimal root-cause fix
      |
      v
GREEN
      |
      v
Broader regression gates
```

Tests must not be weakened to make implementation pass.

For PostgreSQL behaviour, real PostgreSQL tests must be used where the behaviour depends on:

* constraints;
* transactions;
* row locking;
* concurrency;
* deadlocks;
* isolation;
* `NULL` semantics;
* migrations;
* PostgreSQL-specific SQL.

Mock-only evidence is insufficient for those areas.

---

# 6. OpenWA transport architecture — authoritative model

This is a critical architectural point.

OpenWA has **three separate first-class WhatsApp transport paths**:

1. **Baileys**
2. **Chromium / WWebJS**
3. **Official Meta WhatsApp Cloud API**

They are sibling transports.

Baileys does **not** feed into Meta.

WWebJS does **not** feed into Meta.

Meta is not downstream from either browser/non-official provider.

The correct architecture is:

```text
                           Campaign
                              |
                              v
                  Canonical OpenWA Message
                              |
                              v
                       Personalisation
                              |
                              v
                 Campaign Orchestration
                              |
                 Routing / Eligibility
                              |
        +---------------------+---------------------+
        |                     |                     |
        v                     v                     v
   Baileys Adapter       WWebJS Adapter        Meta Adapter
        |                     |                     |
        v                     v                     |
   Baileys Transport     Chromium/WWebJS             |
        |                   Transport                |
        |                     |                     |
        v                     v                     |
     WhatsApp              WhatsApp                 |
                                                    |
                                      +-------------+-------------+
                                      |                           |
                                      v                           v
                              Meta free-form              Meta approved
                           where policy permits          template binding
                                      |                           |
                                      +-------------+-------------+
                                                    |
                                                    v
                                         WhatsApp Cloud API
```

A simplified representation is:

```text
Canonical personalised campaign message
            |
            +------> BAILEYS ------> WhatsApp
            |
            +------> WWEBJS -------> WhatsApp
            |
            +------> META ----------> WhatsApp Cloud API
```

These are independent provider lines.

---

# 7. Campaign transport-selection requirement

Campaigns must support selecting any valid provider subset.

Conceptually:

```text
Baileys only

WWebJS only

Meta only

Baileys + WWebJS

Baileys + Meta

WWebJS + Meta

Baileys + WWebJS + Meta
```

This requirement was explicitly added because the platform should allow a campaign operator to decide which transport estate participates.

The provider subset is then combined with:

* health;
* routing eligibility;
* routing policy;
* available senders/nodes;
* reservations;
* campaign configuration;
* transport-specific constraints.

Selecting all three does **not** mean all three send the same message simultaneously.

The orchestration layer selects eligible transport execution according to configured routing rules.

---

# 8. Canonical message architecture — authoritative invariant

The canonical/generic OpenWA campaign message is the authoritative representation.

Provider implementations adapt that message.

The provider implementation must never redefine the canonical campaign content.

Correct model:

```text
Campaign
    |
    v
Canonical OpenWA message
    |
    v
Personalisation
    |
    +-------------> Baileys representation
    |
    +-------------> WWebJS representation
    |
    +-------------> Meta representation
```

Therefore:

### Baileys

Remains capable of normal personalised/free-form OpenWA messages.

### WWebJS

Remains capable of normal personalised/free-form OpenWA messages.

### Meta

Uses the same canonical personalised content but must adapt it according to Meta's policies.

The Meta branch may therefore choose between:

```text
Meta Adapter
     |
     +----> eligible free-form Meta message
     |
     +----> approved Meta template binding
```

This is a **Meta-provider concern only**.

A Meta template must never become the canonical message definition for the whole campaign.

---

# 9. Why this distinction matters

Consider a campaign configured for:

```text
Baileys + WWebJS + Meta
```

Suppose its canonical content is:

```text
Hello {{first_name}}, your account has been activated.
```

Baileys may render that directly.

WWebJS may render that directly.

Meta may need to map it to something such as:

```text
approved template:
account_activation_v3
```

with typed parameters.

That Meta template binding does not alter what Baileys or WWebJS send.

This must remain true throughout future backend and frontend work.

---

# 10. Transport-neutral orchestration

The architecture uses a shared campaign orchestration/dispatch layer.

It should not become:

```text
Baileys campaign engine
WWebJS campaign engine
Meta campaign engine
```

as three separate campaign systems.

Instead:

```text
                   Campaign Engine
                         |
                         v
                  Routing Decision
                         |
             +-----------+-----------+
             |           |           |
             v           v           v
          Baileys      WWebJS       Meta
          Adapter      Adapter      Adapter
```

This preserves:

* common campaigns;
* common recipients;
* common scheduling;
* common personalisation;
* common execution state;
* common auditability;
* common retry semantics;
* transport-specific adapter behaviour.

---

# 11. Critical delivery-safety invariant

A major safety rule is:

> **Ambiguous/UNKNOWN send outcomes must never cause automatic cross-provider retry.**

Example:

OpenWA submits through Meta.

The request times out.

OpenWA cannot prove whether Meta accepted the message.

Automatically rerouting that recipient to Baileys could result in:

```text
Meta message delivered
+
Baileys message delivered
=
duplicate message
```

Therefore:

```text
Definitive retryable failure
           |
           v
   retry according to policy


Definitive permanent failure
           |
           v
          fail


Ambiguous / UNKNOWN outcome
           |
           v
DO NOT automatically cross-provider resend
```

This rule applies across the entire transport system.

---

# 12. Provider health and eligibility

Being configured for a campaign does not automatically make a provider eligible.

The routing layer must consider provider/sender health.

Conceptually:

```text
Campaign permitted transports
          |
          v
Health / readiness
          |
          v
Routing eligibility
          |
          v
Routing policy
          |
          v
Transport selection
```

Meta template synchronisation is not itself proof that the sender is healthy.

That distinction became important during adversarial review.

---

# 13. Task 6 status before Meta integration

Before the Meta extension was introduced, Task 6 backend production engineering had already advanced substantially.

A long sequence of Task 6 defects/work items approximately through:

```text
T6-001 ... T6-038
```

had been addressed.

Prior validation included combinations of:

* Go tests;
* Docker;
* PostgreSQL;
* migrations;
* gateway tests;
* control-plane tests;
* security verification;
* OpenAPI route checking;
* `go vet`;
* `go build`.

Task 6 local backend engineering was considered substantially complete.

The later Meta requirement extended the backend scope rather than restarting Task 6.

---

# 14. What adding Meta changed

The user then elected to add the **official Meta WhatsApp Cloud API** as an additional provider.

The design deliberately avoided replacing the existing transports.

The scope became:

```text
Existing
  Baileys
  WWebJS

        +

New
  Meta Cloud API

        =

Three-transport OpenWA backend
```

The Meta extension required work across:

* provider domain;
* routing;
* campaign configuration;
* database;
* credentials;
* sender governance;
* templates;
* dispatch;
* delivery tracking;
* webhook ingestion;
* inbound messages;
* STOP;
* admin APIs;
* runtime configuration;
* OpenAPI;
* production compose;
* readiness.

---

# 15. Meta implementation plan

Primary plan:

```text
docs/superpowers/plans/2026-08-11-meta-cloud-provider.md
```

The plan broadly contains:

```text
Task 1
Provider/routing domain

Task 2
Migration 0075 + governed Meta sender

Task 3
Credential resolver + Graph HTTP client

Task 4
Template catalogue + Meta message binding

Task 5
Routing governance / AUTO / WEIGHTED

Task 6
Transport-neutral dispatch + Meta submission

Task 7
Meta webhook + inbound ingestion

Task 8
Admin/runtime/OpenAPI integration

Task 9
Regression + Docker + Neon + handover
```

The implementation has progressed through all architectural sections.

The checkboxes should nevertheless only be updated according to actual final evidence.

---

# 16. Meta provider domain — IMPLEMENTED

The provider/routing domain was extended to represent Meta as a first-class provider.

Concepts added include Meta/Cloud API provider identity and Meta-capable send modes.

Campaign routing can distinguish explicit provider sets.

Routing governance includes concepts such as:

```text
AUTO
WEIGHTED
```

alongside explicit subset selection.

---

# 17. Meta sender model — IMPLEMENTED

Meta senders are represented as governed platform resources.

The model includes provider identity such as:

* organisation;
* WABA;
* phone-number ID;
* credential reference;
* Graph API version;
* sender pool;
* lifecycle state;
* health/verification state.

The intent is that Meta credentials/sender identities are administered through controlled OpenWA governance rather than embedded casually into campaign payloads.

---

# 18. Maker-checker sender lifecycle — IMPLEMENTED / TESTED

A maker-checker style Meta sender lifecycle was implemented.

Administrative operations support governed creation/verification/state changes.

Explicit sender verification may legitimately establish sender health.

This is intentionally different from template synchronisation.

---

# 19. Meta sender identity immutability — HARDENED

A Grok review found that the low-level CAS/store implementation could potentially mutate identity-defining Meta sender fields.

The normal API did not intentionally expose that behaviour, but the storage primitive still permitted it.

A RED regression was created.

The storage layer was then hardened.

Frozen creation identity includes fields such as:

* WABA ID;
* phone-number ID;
* credential reference/key;
* organisation;
* sender pool;
* Graph version;
* equivalent identity-defining fields.

PostgreSQL CAS lifecycle updates no longer rewrite frozen identity columns.

The memory implementation was also hardened.

---

# 20. Meta phone-number identity uniqueness — HARDENED

PostgreSQL already had global uniqueness for:

```text
phone_number_id
```

A Grok claim that PostgreSQL lacked the uniqueness rule was therefore rejected.

However, testing showed the **memory store** did not enforce equivalent behaviour.

A RED regression reproduced duplicate insertion.

The memory implementation was updated to mirror the PostgreSQL invariant.

---

# 21. Meta credential handling — IMPLEMENTED

Meta sender configuration uses credential references/resolvers rather than storing access tokens directly in ordinary campaign data.

Runtime components resolve the actual secret when constructing Meta Graph API requests.

Credential values must not enter:

* campaign payload logs;
* error responses;
* review packets;
* documentation;
* Git.

---

# 22. OpenRouter secret handling

AI reviewer secrets are kept outside the repository.

Known location:

```text
C:\Users\sanus\ai-engineering-secrets
```

Known helper files:

```text
openrouter.env.txt
check-openrouter-env.ps1
set-openrouter-env.ps1
```

The checker reports only secret presence/length.

A previous tool invocation accidentally exposed the actual key in tool output.

That value must be treated as compromised/sensitive historical output and must never be reproduced in documentation or responses.

---

# 23. Temporary PostgreSQL secret residue

An untracked temporary file existed during testing:

```text
.tmp-meta-pg-password
```

It was explicitly removed.

It was not removed using a broad `git clean`.

A final residue scan still needs to be rerun before completion.

---

# 24. Meta Graph client — IMPLEMENTED

A hardened Meta Graph HTTP client was added.

The implementation classifies send outcomes so the orchestration layer can make safe retry decisions.

---

# 25. HTTP 429 handling — IMPLEMENTED

Meta HTTP:

```text
429 Too Many Requests
```

is treated as a retryable provider condition according to policy.

This is distinct from an ambiguous submission.

---

# 26. HTTP 408 ambiguity defect — FOUND AND FIXED

The Grok council found that:

```text
HTTP 408
```

was originally classified as a permanent failure.

That is unsafe.

An HTTP timeout does not prove that Meta did not accept the request.

A RED regression demonstrated the classification problem.

The classifier was corrected so 408 produces:

```text
OUTCOME_UNKNOWN
```

This prevents unsafe automatic cross-provider resend.

---

# 27. Meta 5xx / ambiguous transport semantics

Where the system cannot determine whether Meta accepted a submission, the result is conservatively treated as ambiguous rather than assumed safe to resend.

This includes relevant 5xx/transport failure scenarios.

The essential invariant is:

```text
unknown provider acceptance
!=
safe retry via another provider
```

---

# 28. Provider error-detail leakage — FOUND AND FIXED

A RED regression demonstrated that Meta/provider-controlled error content could enter the application's API error detail.

A malicious or malformed upstream response could contain token-like or sensitive material.

Rather than trying to maintain a fragile secret-redaction parser, the error path was made conservative.

Raw provider-controlled error detail is no longer propagated directly as normal user/application-visible detail.

Application-controlled safe error detail is used.

---

# 29. Meta material loading — IMPLEMENTED

Meta sender/material resolution was separated from OpenWA browser-session allocation.

A Meta send must not unnecessarily allocate:

* a Baileys session;
* a WWebJS browser;
* Chromium material.

Meta is an independent transport.

---

# 30. Template catalogue — IMPLEMENTED

Meta template catalogue support was added.

Templates are:

**provider-specific Meta representations**

and not the platform's canonical campaign content.

The catalogue supports provider template metadata needed for approved Meta sending.

---

# 31. Typed Meta component bindings — IMPLEMENTED

Meta message/template binding moved toward typed components rather than opaque JSON blobs.

This enables explicit handling of provider-specific content such as:

* headers;
* body parameters;
* button parameters;
* other Meta components.

---

# 32. Template JSON Base64 defect — FOUND AND FIXED

Template JSON represented as:

```go
[]byte
```

was serialising into Base64 in JSON output.

Where raw JSON semantics were required, this was changed to:

```go
json.RawMessage
```

This corrected the representation.

---

# 33. Template sync stale-write defect — FOUND AND FIXED

The Grok council identified a real concurrency/durability problem.

Scenario:

```text
Sync A starts
Sync B starts later
Sync B completes with newer catalogue
Sync A completes afterward
```

Without generation control, Sync A could overwrite the newer state.

A PostgreSQL RED regression reproduced the issue.

An earlier timestamp-based approach using something equivalent to:

```text
MAX(last_synced_at)
```

was insufficient because a valid newer sync may intentionally produce an **empty catalogue**.

If no catalogue rows exist, row timestamps cannot represent authoritative generation.

The durable fix introduced explicit per-WABA template sync state/generation.

Result:

```text
new generation wins
old generation cannot overwrite it
empty newer catalogue remains authoritative
```

This hardening is associated with migration 0077.

---

# 34. Template sync sender-health defect — FOUND AND FIXED

The review found that template synchronisation could implicitly rehabilitate sender health.

That was incorrect.

Template availability and sender verification are separate concerns.

A RED test reproduced the behaviour.

After the fix:

```text
template sync
     |
     v
catalogue update

NOT

sender health mutation
```

Only explicit sender verification/lifecycle processes should alter verification-related health.

---

# 35. Explicit `/verify` behaviour — REVIEW FINDING REJECTED

A reviewer suggested explicit sender verification itself should not be able to make a sender healthy.

That claim was rejected.

Explicit verification of the exact configured Meta WABA/phone identity is intentionally a valid sender-health operation.

The actual defect was template sync implicitly modifying health.

---

# 36. Meta message binding — IMPLEMENTED

Canonical OpenWA content can be associated with provider-specific Meta representation.

The architecture keeps:

```text
canonical campaign/message version
```

separate from:

```text
Meta template binding
```

The binding is intended to remain immutable relative to the frozen campaign/message version where required for deterministic execution.

---

# 37. Routing governance — IMPLEMENTED

Provider selection supports explicit subsets and routing governance.

The design includes:

```text
AUTO
WEIGHTED
```

and explicit provider configurations.

Provider health governs actual eligibility.

---

# 38. Campaign Meta route coherence defect — FOUND AND FIXED

A real problem was identified where a campaign could contain:

```text
meta_sender_id
```

without coherent Meta provider selection.

This could produce ambiguous execution evidence.

For example:

```text
Meta sender configured
+
provider not explicitly Meta
```

could potentially fall into a browser/OpenWA allocation path.

That is invalid.

The invariant is now enforced at two levels.

---

# 39. Route coherence — application layer

Application validation fails closed when Meta-specific endpoint evidence exists but the campaign's transport configuration does not support Meta coherently.

This prevents invalid routes from being created through normal service paths.

---

# 40. Route coherence — database layer

A PostgreSQL constraint independently enforces the same concept.

This protects the invariant against:

* direct SQL;
* future code paths;
* service bugs;
* migration mistakes.

Application validation is not the sole line of defence.

---

# 41. PostgreSQL CHECK / NULL defect — FOUND AND FIXED

The first route-coherence database constraint contained a subtle PostgreSQL problem.

SQL CHECK constraints do not reject an expression merely because it evaluates to `NULL`.

They reject explicit `FALSE`.

Therefore:

```text
transport_provider = NULL
```

could cause a compound expression to evaluate to SQL `NULL` and pass the constraint.

A regression reproduced this.

The expression was hardened using explicit false/`COALESCE` semantics.

Invalid Meta evidence must now evaluate to `FALSE`, not `NULL`.

---

# 42. Routing release TOCTOU review claim — REJECTED

A Grok reviewer raised a possible reservation-release race.

The actual PostgreSQL implementation was inspected.

The relevant campaign/status check occurs inside the transaction with appropriate locking/revalidation.

The claimed defect was not reproduced and was rejected.

No code change should be made merely to satisfy this rejected finding.

---

# 43. Transport-neutral dispatch — IMPLEMENTED

The dispatch layer was extended so Meta submission participates in the existing transport-neutral execution architecture.

Meta is therefore not implemented as an isolated campaign system.

The dispatch pipeline can select an eligible transport and invoke the corresponding adapter.

---

# 44. Meta webhook ingestion — IMPLEMENTED

Meta webhook support includes provider-specific endpoint handling.

Implemented areas include:

* verification challenge;
* signature verification;
* raw-body validation;
* sender/WABA/phone binding;
* status callbacks;
* inbound messages;
* idempotency;
* unsupported-event tolerance;
* STOP integration;
* request size controls.

---

# 45. Raw-body HMAC verification — IMPLEMENTED

Meta webhook HMAC uses the raw request body.

The body must not be parsed and reserialised before signature verification.

The raw bytes are verified against the provider signature first.

---

# 46. Webhook body limit review claim — REJECTED

A reviewer claimed the production webhook body limit could be zero.

Inspection showed the server constructor provides the default.

The relevant default observed was approximately:

```text
1 MiB
```

Therefore this particular claim was rejected.

---

# 47. Invalid webhook signatures — HANDLED

Requests with invalid Meta signatures are rejected.

Unsigned or incorrectly signed evidence must not be admitted into delivery/inbound state.

---

# 48. Webhook verification challenge — IMPLEMENTED

Meta's webhook verification/challenge path is implemented separately from ordinary signed event ingestion.

---

# 49. Malformed webhook item poisoning — FOUND AND FIXED

A valid signed Meta webhook batch can contain multiple items.

The earlier implementation could allow one malformed individual item to abort handling of otherwise valid evidence.

Example:

```text
signed batch
   |
   +-- valid delivery status
   |
   +-- malformed item
   |
   +-- valid inbound message
```

The malformed item must not discard the valid status or inbound message.

A RED test reproduced the problem.

The implementation now isolates malformed individual items.

The valid siblings continue to be processed.

---

# 50. Unsupported Meta webhook variants

Unsupported or irrelevant variants are ignored safely where appropriate.

They should not poison otherwise valid batch processing.

---

# 51. Meta delivery-status `wamid` isolation defect — FOUND AND FIXED

This was one of the more important multi-tenant findings.

Initially, status callbacks could resolve a provider message ID through a global lookup similar to:

```text
GetByProviderMessageID(wamid)
```

That was not sufficiently scoped.

A provider message ID should be interpreted together with the authenticated/configured Meta sender and campaign ownership.

Multiple Grok reviewers independently raised this concern.

---

# 52. Sender-scoped delivery resolver — IMPLEMENTED

Meta delivery status now requires a scoped resolution path involving:

* authenticated/configured Meta sender;
* organisation;
* campaign;
* frozen Meta route evidence;
* provider message ID.

PostgreSQL regression evidence demonstrated:

```text
correct sender + own wamid
        -> resolves

wrong sender / wrong organisation
        -> ErrRecipientNotFound
```

The persistent control-api runtime was wired to use the scoped resolver.

The Meta HTTP handler no longer relies on the unsafe global provider-message-ID lookup.

Memory/test environments fail closed where the scoped resolver is not provided.

---

# 53. Inbound Meta messages — IMPLEMENTED

Inbound Meta messages are fed into the platform's inbound handling path rather than being treated as an isolated subsystem.

---

# 54. STOP / opt-out integration — IMPLEMENTED

Meta inbound STOP handling uses the platform's existing:

```text
OptOutProcessor
```

This is deliberate.

There should not be separate inconsistent unsubscribe semantics for each provider.

---

# 55. Public webhook network handling

The Meta webhook endpoint includes the required public network exception/configuration so that Meta callbacks are not incorrectly blocked by controls intended for internal/admin surfaces.

This remains provider-specific network exposure, not a relaxation of the whole control API.

---

# 56. PostgreSQL lock-order deadlock — FOUND AND FIXED

A deterministic two-transaction test reproduced a real database deadlock.

PostgreSQL returned:

```text
SQLSTATE 40P01
```

The competing operations acquired locks in different orders.

One path effectively acquired:

```text
plan/reservation
then campaign
```

while another lifecycle path acquired:

```text
campaign
then related routing state
```

---

# 57. Deadlock fix

The operations were normalised to a campaign-first lock order.

The corrected conceptual flow is:

```text
identify campaign
      |
      v
lock campaign
      |
      v
load/lock plan and reservations
      |
      v
revalidate ownership/state
      |
      v
perform release
```

Regression tests also preserve:

* active-campaign semantics;
* ownership verification;
* idempotent release behaviour.

---

# 58. Control API schema-readiness defect — FOUND AND FIXED

Before hardening, control-api readiness could succeed against a database that did not include all required Meta/hardening migrations.

This could allow:

```text
application reports ready
+
schema is too old
```

A RED test demonstrated the problem.

Readiness was updated to verify required Meta-era schema structures.

Evidence demonstrated:

```text
migration-76 schema
     -> rejected

migration-77 schema
     -> accepted
```

---

# 59. Runtime wiring — IMPLEMENTED

Meta support has been wired into relevant runtime binaries including:

* control API;
* campaign worker.

Runtime configuration supports Meta provider material where configured.

Provider capability/bootstrap wiring was added.

---

# 60. Production compose Meta support — IMPLEMENTED / NEEDS FINAL RECHECK

An optional production Compose overlay/configuration was introduced for Meta-related runtime material.

Earlier compose verification was green.

Because council fixes occurred afterward, the full compose/security verifier must be rerun before declaring final completion.

---

# 61. Migration 0075

Migration 0075 introduced foundational Meta schema elements including governed Meta sender support.

It has been exercised on:

* local PostgreSQL 17;
* Neon PostgreSQL 18 compatibility validation.

---

# 62. Migration 0076

Migration 0076 expanded Meta schema/binding capabilities, including template-related structures.

It was also exercised on:

* local PostgreSQL 17;
* Neon PostgreSQL 18 compatibility validation.

---

# 63. Migration 0077

Migration 0077 contains post-adversarial-review hardening.

At minimum it includes:

* durable Meta template sync generation/state;
* campaign Meta provider/sender route-coherence constraint;
* associated supporting database objects.

The exact filename should be read directly from the repository before being quoted in a subsequent formal document.

Do not invent the filename from memory.

---

# 64. Fresh PostgreSQL 17 migration verification — VERIFIED

After the post-Grok fixes, two **new disposable PostgreSQL 17 databases** were migrated from zero.

Both completed:

```text
77 / 77 migrations
```

Both resulted in exactly:

```text
133 public tables
468 indexes
2,112 constraints
```

The identical result across two clean databases is strong evidence of deterministic PostgreSQL 17 migration behaviour through migration 0077.

---

# 65. Post-council PostgreSQL regression verification — VERIFIED

A combined regression set was run after the council hardening using the migration-77 PostgreSQL database.

Relevant packages included:

```text
internal/metacloud
internal/dispatch
internal/execution
internal/platform/httpserver
cmd/control-api
cmd/campaign-worker
```

Those targeted post-hardening regressions were green at that checkpoint.

---

# 66. Neon environment

Neon project:

```text
lucky-credit-36128290
```

Project title:

```text
Test2
```

Preserved validation branch:

```text
openwa-0828-validation
```

Branch ID:

```text
br-green-pine-aykv09eo
```

Observed PostgreSQL version:

```text
18.4
```

The preserved database:

```text
neondb
```

must remain unchanged.

---

# 67. Neon 0075 / 0076 evidence — VERIFIED COMPATIBILITY

Migrations 0075 and 0076 were previously applied against the Neon PG18 environment.

They were then reversed so the preserved validation database returned to its pre-Meta state.

The preserved `neondb` was checked afterward and showed no Meta sender residue.

This proves **migration compatibility for 0075/0076 on PG18**, not a complete persistent Meta deployment.

---

# 68. Full zero-to-0077 Neon attempt

A unique disposable Neon branch was created to try the full migration chain.

Connector/prepared-statement/tool boundaries prevented completion of the entire chain from zero there.

This limitation must not be concealed.

---

# 69. Disposable Neon database for migration 0077

To avoid modifying the preserved `neondb`, a disposable database clone was created:

```text
openwa_meta77_validate
```

Its baseline was approximately:

```text
PostgreSQL 18.4
129 tables
411 indexes
2,003 constraints
```

The only required 0077 prerequisite missing from the clone was:

```text
campaigns.meta_sender_id
```

because the preserved baseline had intentionally had Meta migrations removed.

---

# 70. Neon PG18 migration 0077 validation — VERIFIED COMPATIBILITY

Because 0075 and 0076 had already been independently proven compatible with PG18, the missing prerequisite was added only to the disposable validation clone.

The exact migration 0077 hardening DDL was then applied transactionally.

PostgreSQL 18.4 accepted it.

Validation showed:

```text
Meta sync-state table present
route-coherence constraint validated
meta_sender_id present
```

Resulting counts:

```text
130 tables
412 indexes
2,011 constraints
```

---

# 71. Correct statement of Neon evidence

The correct conclusion is:

> The entire 0001–0077 migration chain has been proven from zero twice on PostgreSQL 17. Migrations 0075 and 0076 have separately been proven compatible with Neon PostgreSQL 18, and migration 0077 has separately been proven compatible with PostgreSQL 18 on a disposable Neon clone. A complete zero-to-0077 migration replay has **not yet been completed on Neon**.

Do not change that statement to “Neon 77/77 passed”.

It did not.

---

# 72. Disposable Neon resources

Potential disposable resources created during validation include:

```text
openwa_meta77_validate
```

and a temporary admin database used for cloning.

An empty branch was also created:

```text
openwa-meta-final-neon-20260813
```

Branch ID:

```text
br-wandering-truth-ayej7koi
```

Another empty branch was observed:

```text
openwa-0828-final-validation
```

Branch ID:

```text
br-curly-truth-ayj5uxa6
```

Only resources unquestionably created for this validation effort should be deleted.

Preserve:

```text
openwa-0828-validation
br-green-pine-aykv09eo
neondb
```

---

# 73. Docker cleanup already performed

Docker storage had grown significantly during Task 6 testing.

Approximate initial usage was:

```text
Images   ~34.1 GB
Volumes  ~49.8 GB
```

Cleanup was deliberately **OpenWA-specific**.

No global Docker prune was used.

After the first targeted cleanup, approximate usage was:

```text
Images   ~29.41 GB
Volumes  ~47.0 GB
```

Other user projects were intentionally preserved.

---

# 74. Other Docker projects must remain untouched

In particular, previous cleanup preserved projects such as:

```text
InvestNaija
SocialForge
```

Any further cleanup must continue to target only disposable OpenWA artifacts.

Do not run a broad:

```text
docker system prune
docker volume prune
```

against the machine.

---

# 75. `campaign_execution_events` test-event storm

A large OpenWA test artefact accumulation was discovered in:

```text
campaign_execution_events
```

Original row count:

```text
2,288,193
```

Approximate table size:

```text
684 MB
```

Of these:

```text
2,288,187
```

were:

```text
COMPLETION_ASSESSMENT_FAILED
```

test-generated records.

Six READY records were legitimate/retained.

---

# 76. Test-event cleanup procedure — COMPLETED

Cleanup was deliberately constrained.

The process was:

```text
pause only OpenWA worker
        |
delete exactly 2,288,187 failed test rows
        |
retain six READY rows
        |
VACUUM FULL ANALYZE
        |
restart OpenWA worker
```

The table reduced to approximately:

```text
48 KB
```

The associated campaigns were test campaigns such as:

```text
Sender health integration
```

Other projects were untouched.

---

# 77. Active OpenWA stack status

At the earlier verified checkpoint:

```text
13 / 13 OpenWA services healthy
```

This is **historical evidence**, not the final post-council completion gate.

It must be checked again before final completion.

---

# 78. OpenAPI parity

Before the latest council changes, route parity verification produced:

```text
294 / 294
```

implemented `/api/v1` method/path pairs.

No duplicate prefixes were detected.

Because code changed afterward, OpenAPI parity must be rerun.

Do not assume the final count remains 294 if legitimate later routes were added.

Use the actual output.

---

# 79. Production compose security verification

Earlier verification found approximately:

```text
8 services
25 mandatory secrets/configuration requirements
```

and the verifier passed.

This evidence predates some later hardening changes.

It therefore belongs in:

**previously green; needs fresh final rerun**

rather than:

**final verified**

---

# 80. Committed-secret scan

A committed-secret scan was previously green in the correct environment with Git available.

This must be rerun after all latest changes.

---

# 81. Gateway secret-file test

Gateway secret-file handling previously passed.

It must also be included in the final fresh security gate.

---

# 82. `git diff --check`

`git diff --check` was clean at an earlier post-council point.

Subsequent changes occurred.

Therefore a new:

```text
git diff --check
```

must be taken before completion.

---

# 83. Race testing

Focused `-race` testing was green before some of the later council fixes.

Because later work included concurrency-sensitive changes—especially template generations, webhook processing and PostgreSQL routing/release behaviour—race verification must be rerun.

At minimum target:

```text
internal/metacloud
internal/dispatch
internal/execution
relevant internal/platform/httpserver paths
```

plus other affected packages as appropriate.

---

# 84. `go vet`

Full `go vet` had previously passed.

Because code changed later, rerun it.

---

# 85. `go build`

Full build had previously passed.

Again, this must be repeated against the final hardened worktree.

---

# 86. Full PostgreSQL opt-in suite discovery

During final verification, approximately:

```text
38
```

PostgreSQL opt-in test environment flags were identified.

This matters because a normal:

```text
go test ./...
```

could otherwise leave PostgreSQL acceptance tests skipped.

The final validation harness therefore enabled all discovered PostgreSQL opt-in suites.

---

# 87. Strongest full-repository verifier

A named Docker verifier was launched:

```text
openwa-meta77-full-go
```

It was intended to execute:

```text
go test -p=1 -count=1 ./...
```

with the PostgreSQL opt-in test environments enabled against a pristine migration-77 database.

Serial execution was deliberate.

The container was given a stable name so its result could survive Desktop Commander disconnection.

---

# 88. Current exact unresolved verification state

Desktop Commander became unstable while the long full-suite verifier was running.

The command session later appeared to have ended, suggesting the test probably completed.

However, the actual Docker state/exit code/log was never successfully recovered because the Desktop Commander tool binding repeatedly failed.

Therefore:

```text
openwa-meta77-full-go
```

must currently be classified:

# UNKNOWN

Not:

```text
passed
```

and not:

```text
failed
```

---

# 89. First command when Desktop Commander works

Do **not** immediately rerun the full suite.

First recover the existing evidence.

Conceptually:

```text
docker ps -a --filter name=openwa-meta77-full-go
```

Then inspect its state/exit code:

```text
docker inspect openwa-meta77-full-go
```

Specifically capture:

* status;
* exit code;
* Docker state error;
* finished time.

Then retrieve its logs.

Only rerun the suite if the existing result is genuinely absent/unrecoverable.

---

# 90. What to do if the full suite failed

Do not make a speculative fix.

Use systematic debugging.

Sequence:

```text
identify exact failing test
        |
reproduce narrow failure
        |
determine code vs environment failure
        |
preserve RED regression
        |
fix root cause
        |
GREEN narrow test
        |
GREEN package
        |
rerun broad/full gate
```

No test should be disabled simply to obtain a green full-suite result.

---

# 91. Grok 4.6 adversarial review council

The Meta/backend candidate underwent an independent multi-model review.

The user specifically requested multiple simultaneous Grok 4.6 instances operating as a review council.

Exact OpenRouter model route:

```text
x-ai/grok-4.6
```

---

# 92. Grok council structure

Four independent Grok 4.6 reviewers were run concurrently.

They were blind to one another.

Their specialist perspectives were approximately:

```text
Reviewer 1
Correctness / distributed systems

Reviewer 2
Security / adversarial

Reviewer 3
Database / durability

Reviewer 4
Architecture / integration
```

A fifth challenge reviewer was contemplated as an optional later stage.

---

# 93. Reviewer project context

The reviewers were not simply given isolated code fragments.

A common project-context brief explained:

* OpenWA's purpose;
* production/multi-tenant expectations;
* the three independent transport model;
* Baileys;
* WWebJS;
* Meta Cloud API;
* canonical campaign-content invariant;
* provider-specific Meta template binding;
* UNKNOWN/no-auto-cross-provider-retry rule;
* implemented versus external acceptance;
* dirty-worktree constraints;
* relevant current architecture;
* review scope.

This context should be preserved in the final reviewer rerun.

---

# 94. Grok packet execution

An initial monolithic packet was approximately:

```text
277 KB
```

and returned HTTP 400.

A tiny route-health call then confirmed:

```text
x-ai/grok-4.6
```

was working.

The code was split into bounded specialist packets around:

```text
54 KB
69 KB
56 KB
90 KB
```

An initial PowerShell concurrent runner failed locally because of packet-path parsing before it reached OpenRouter.

This was not a Grok failure.

A Node concurrent runner replaced it.

The four reviews then completed independently.

---

# 95. Validated Grok defects

The first Grok council produced several real defects that were adjudicated and fixed.

The validated set currently includes:

### 95.1 HTTP 408 duplicate-send ambiguity

Fixed to UNKNOWN.

### 95.2 Malformed webhook item poisoning valid batch

Fixed with item-level isolation.

### 95.3 Template sync silently rehabilitating sender health

Fixed.

### 95.4 Stale template catalogue overwriting newer state

Fixed using durable sync generation.

### 95.5 Meta endpoint evidence with incoherent provider selection

Fixed application-side.

### 95.6 PostgreSQL route-coherence enforcement

Added/hardened database-side.

### 95.7 PostgreSQL CHECK `NULL` loophole

Fixed.

### 95.8 Meta delivery callback insufficiently sender-bound

Fixed through sender/org/route-scoped resolution.

### 95.9 Control-api readiness accepting old schema

Fixed.

### 95.10 PostgreSQL `40P01` lock-order deadlock

Reproduced and fixed.

### 95.11 Meta sender identity mutable through low-level CAS

Fixed.

### 95.12 Memory-store phone uniqueness mismatch

Fixed.

### 95.13 Raw provider error-detail leakage risk

Fixed conservatively.

The precise number may be represented differently depending on whether the route-coherence application and DB issues are counted together or separately.

The important point is to preserve each regression individually.

---

# 96. Rejected Grok findings

The reviewers were deliberately treated as advisers, not authorities.

Claims were validated against the code/tests.

Rejected examples include:

### “Meta webhook body limit is zero in production”

Rejected.

The server constructor supplies the default limit.

### “Routing-release TOCTOU vulnerability”

Rejected after examining the transactional PostgreSQL implementation.

### “PostgreSQL lacks phone-number uniqueness”

Rejected.

The PostgreSQL constraint already existed.

The real inconsistency was memory storage.

### “Explicit `/verify` must not make a sender healthy”

Rejected.

Explicit verification is intentionally capable of validating sender health.

Template sync was the inappropriate mutation.

---

# 97. Reviewer-fleet configuration

Reviewer evidence/configuration exists under:

```text
C:\Users\sanus\OpenWA\campaign-platform-active\validation\ai-review
```

Reviewer fleet configuration:

```text
reviewer-fleet.json
```

---

# 98. Controlled Qwen / GLM calibration pair

The original controlled comparison pair is:

### Qwen

```text
qwen/qwen3.8-max
```

### GLM

```text
z-ai/glm-5.2
```

They should receive:

* the same review packet;
* the same role;
* the same instructions;
* independently;
* blind to each other's output.

Do not specialise one while the calibration study is continuing.

---

# 99. Reviewer calibration metrics

Track at least:

* unique valid Critical findings;
* unique valid Important findings;
* duplicate findings;
* false positives;
* rejected findings;
* findings another model missed;
* later defects that a reviewer should have spotted.

The goal is empirical reviewer quality rather than subjective impressions.

---

# 100. Gemini reviewer

The configured reviewer fleet also includes an additional model such as:

```text
google/gemini-3.6-flash
```

It can provide another independent perspective.

It should not replace either:

* the controlled Qwen/GLM pair;
* the Grok specialist council.

---

# 101. Final reviewer rerun still required

The first Grok council reviewed the earlier Meta candidate.

Those findings were subsequently fixed.

Therefore the **hardened candidate itself** should receive another review.

Correct order:

```text
recover full suite
      |
      v
finish local verification
      |
      v
create updated reviewer packets
      |
      v
4 independent Grok 4.6 reviews
      |
      v
adjudicate Critical/Important findings
      |
      +--> valid -> RED/fix/GREEN
      |
      +--> invalid -> record rejection
      |
      v
Qwen + GLM controlled review
```

If fixes occur, affected/full verification must be repeated afterward.

---

# 102. What remains locally before declaring Meta backend engineering complete

The implementation is advanced.

However, the following evidence still needs to be obtained or refreshed:

```text
Recover openwa-meta77-full-go result

Full Go suite confirmed green

All PostgreSQL opt-in suites confirmed green

Fresh go vet

Fresh go build

Fresh focused race suite

Fresh production compose verifier

Fresh mandatory-secret verification

Fresh committed-secret scan

Fresh gateway secret-file test

Fresh OpenAPI parity

Fresh git diff --check

Fresh temporary-secret residue scan

Fresh active OpenWA stack health check

Confirm other Docker projects remain untouched

Final hardened Grok council rerun

Qwen/GLM calibration review

Adjudicate all new Critical/Important claims

Repeat affected gates if reviewer fixes are made

Reconcile Meta implementation plan

Update adversarial-findings document

Update final handover evidence
```

This is principally a **verification and hardening completion phase**, not a major missing-feature phase.

---

# 103. Local backend completion versus production acceptance

This distinction is critical.

There are two separate milestones.

## A. Local backend engineering completion

Means the implementation has passed:

* source tests;
* PostgreSQL tests;
* migrations;
* vet;
* build;
* race;
* security;
* OpenAPI;
* adversarial review;
* documentation reconciliation.

## B. Production/live acceptance

Requires actual external systems.

The two must never be conflated.

---

# 104. Baileys live acceptance still outstanding

External evidence still required includes areas such as:

* genuine pairing;
* genuine authentication;
* reconnect;
* real sends;
* real media;
* delivery state;
* inbound messages;
* real failures;
* long-running behaviour.

---

# 105. WWebJS live acceptance still outstanding

External evidence still required includes:

* actual Chromium/browser session;
* genuine WhatsApp Web auth;
* reconnection;
* browser stability;
* real messages;
* media;
* inbound;
* status behaviour;
* long-duration operation.

---

# 106. Meta Cloud live acceptance still outstanding

Real Meta acceptance requires:

* valid Meta Business setup;
* WABA;
* real phone-number ID;
* actual production/test access token;
* approved templates;
* live Graph API;
* real send;
* real signed webhook;
* genuine status callback;
* genuine inbound message;
* STOP;
* actual Meta errors/rate limits;
* template lifecycle;
* delivery reconciliation.

The local simulated/backend implementation cannot substitute for this.

---

# 107. Multi-provider live acceptance still outstanding

The final system also needs real validation of scenarios involving combinations of transports.

Examples include:

```text
Baileys + WWebJS

Baileys + Meta

WWebJS + Meta

all three
```

Particular attention should be paid to ambiguous outcomes and fallback rules.

---

# 108. No duplicate-send acceptance

Production acceptance must demonstrate that:

```text
UNKNOWN
```

does not silently become:

```text
retry another provider
```

This is one of the platform's most important safety properties.

---

# 109. Volume/resource testing outstanding

Target-load testing remains external.

It should cover:

* campaign volume;
* throughput;
* node utilisation;
* browser resource consumption;
* queue depth;
* database pressure;
* Redis;
* object/media workload;
* Meta rate limits;
* routing;
* backpressure;
* worker recovery.

---

# 110. Resilience/endurance testing outstanding

Still required:

* long-duration execution;
* restart behaviour;
* reconnect;
* provider outage;
* partial outage;
* database interruptions;
* Redis interruptions;
* network uncertainty;
* browser crashes;
* worker recovery.

---

# 111. DR / operational acceptance outstanding

Before production acceptance, the project still needs real evidence for:

* backups;
* restore;
* RPO;
* RTO;
* disaster recovery;
* monitoring;
* alerts;
* operational dashboards;
* incident response;
* runbooks;
* on-call model.

---

# 112. Security acceptance outstanding

Automated security tests are not an independent penetration test.

A separate security/penetration review remains an external gate.

---

# 113. Deployment acceptance outstanding

Real Railway/Hostinger/other production environment deployment/network evidence remains a separate phase.

Local Docker success does not prove production deployment behaviour.

---

# 114. Frontend boundary

The current checkpoint is primarily backend.

Frontend work must ultimately expose the provider configuration in a way consistent with the architecture.

The UI must understand that:

```text
Baileys
WWebJS
Meta
```

are three independent provider choices.

It must never suggest that Meta is a sub-provider of Baileys or WWebJS.

---

# 115. Frontend campaign provider selection

When frontend work resumes, a campaign should be able to select allowed provider sets consistent with backend support.

Conceptually:

```text
[ ] Baileys
[ ] WWebJS
[ ] Meta Cloud API
```

with routing policy controls where appropriate.

The user should not need to rewrite a Baileys/WWebJS campaign as a Meta template merely because Meta is enabled.

---

# 116. Meta template UX boundary

Where Meta requires an approved template, the UI can expose the Meta-specific binding.

Conceptually:

```text
Canonical campaign message

Baileys:
automatic free-form rendering

WWebJS:
automatic free-form rendering

Meta:
[ eligible free-form ]
or
[ approved template binding ]
```

Again, the Meta representation is provider-specific.

---

# 117. Database design principle reinforced by the review

Critical transport/multi-tenant invariants should be enforced in more than one layer where practical.

For example:

```text
application route validation
+
database CHECK constraint
```

is stronger than relying entirely on the HTTP API.

This principle should continue.

---

# 118. Readiness-design principle

Readiness should represent actual runtime compatibility.

It should not mean merely:

```text
database TCP connection works
```

If a binary requires migration 0077 structures, readiness should fail against migration 0076.

That behaviour is now covered.

---

# 119. Meta identity principle

Provider sender identity must remain immutable after creation unless there is an explicitly designed identity-replacement workflow.

Lifecycle operations should not silently transform one sender identity into another.

---

# 120. Catalogue-generation principle

External synchronisation should not rely solely on row timestamps when an empty result can be authoritative.

Explicit durable generation/state is required where stale-write ordering matters.

That design is now present for Meta templates.

---

# 121. Webhook-processing principle

A signed batch is not necessarily all-or-nothing at the item-validation level.

One malformed provider item should not cause otherwise valid signed evidence in the same batch to disappear.

Item isolation is therefore an important ingestion property.

---

# 122. Delivery-evidence isolation principle

A `wamid` by itself should not be trusted globally for multi-tenant state mutation.

The provider callback must be tied back to the configured sender/tenant/route evidence before mutating recipient delivery state.

---

# 123. Current status by area

## Core Task 6 backend

**Status:** substantially implemented and previously locally validated.

## Baileys provider

**Status:** backend implementation available; real/live acceptance outstanding.

## WWebJS provider

**Status:** backend implementation available; real/live acceptance outstanding.

## Meta provider

**Status:** implementation substantially complete; final post-hardening local gates outstanding.

## Meta sender governance

**Status:** implemented and hardened.

## Meta Graph client

**Status:** implemented and ambiguity/error hardened.

## Meta templates

**Status:** implemented and concurrency hardened.

## Meta dispatch

**Status:** implemented.

## Meta webhook

**Status:** implemented and adversarially hardened.

## Meta status callbacks

**Status:** implemented with sender-scoped resolution.

## Meta inbound/STOP

**Status:** implemented.

## Routing/provider subsets

**Status:** implemented.

## Route coherence

**Status:** application + database protection implemented.

## Migrations 0001–0077 PG17

**Status:** verified from zero twice.

## PG18 compatibility

**Status:** 0075/0076 and 0077 independently validated.

## Full zero-to-0077 Neon run

**Status:** not completed.

## Full repository final post-hardening test

**Status:** result unknown until `openwa-meta77-full-go` recovered.

## Fresh vet/build/race/security/OpenAPI

**Status:** outstanding.

## Final hardened reviewer rerun

**Status:** outstanding.

## Production/live acceptance

**Status:** outstanding.

---

# 124. Immediate continuation procedure

When a working Desktop Commander session is available, the next agent should proceed in the following order.

### 124.1 Establish actual machine/repository state

Check:

```text
device connected
repo exists
current branch
git status
```

Do not mutate anything.

### 124.2 Recover the named verifier

Inspect:

```text
openwa-meta77-full-go
```

before launching another full test.

### 124.3 Capture its evidence

Record:

* status;
* exit code;
* finished timestamp;
* logs;
* failing package/test if any.

### 124.4 Only debug if it failed

Use strict RED/fix/GREEN.

### 124.5 Run fresh static/build gates

Run:

```text
go vet
go build
```

against the actual final candidate.

### 124.6 Run race verification

At minimum cover affected provider/dispatch/execution/webhook areas.

### 124.7 Rerun security/compose gates

Include:

* production compose verifier;
* mandatory secret check;
* secret-file handling;
* committed-secret scan;
* temporary residue check.

### 124.8 Rerun OpenAPI parity

Use current actual route counts.

### 124.9 Rerun Git hygiene check

```text
git diff --check
```

### 124.10 Verify runtime health

Confirm active OpenWA stack.

Earlier reference:

```text
13 / 13 healthy
```

but obtain a fresh result.

Also confirm InvestNaija/SocialForge/other projects remain untouched.

---

# 125. Final reviewer procedure

After local gates are green:

Create updated reviewer packets from the **current hardened candidate**.

Run:

```text
Grok 4.6 correctness/distributed systems
Grok 4.6 security/adversarial
Grok 4.6 database/durability
Grok 4.6 architecture/integration
```

All independently.

All exact:

```text
x-ai/grok-4.6
```

All blind to one another.

---

# 126. Reviewer adjudication procedure

For every Critical/Important claim:

```text
reviewer claim
      |
      v
inspect actual source
      |
      v
can defect be reproduced?
      |
   +--+--+
   |     |
  yes    no
   |     |
 RED   reject/record
   |
 fix
   |
 GREEN
```

Reviewer output must not be treated as truth without evidence.

---

# 127. Qwen / GLM procedure

Afterward run the controlled same-role packet independently through:

```text
qwen/qwen3.8-max
z-ai/glm-5.2
```

Do not give either reviewer the other's findings.

Continue collecting comparative reviewer-quality evidence.

---

# 128. Verification after reviewer fixes

If the final reviewer pass causes any implementation change:

rerun:

* affected tests;
* PostgreSQL tests;
* race where applicable;
* vet;
* build;
* security if applicable;
* OpenAPI if applicable;
* full suite where justified.

Do not retain a green gate from before the final code change as final evidence.

---

# 129. Neon final step

If a safe method becomes available for replaying the entire migration chain from zero against a disposable Neon PG18 database, it would strengthen the evidence.

However, it is not acceptable to falsely claim this was already achieved.

Current truthful evidence is:

```text
PG17:
0001–0077 from zero
PASS twice

PG18:
0075/0076 compatibility
PASS

PG18:
0077 compatibility
PASS

PG18:
0001–0077 from zero
NOT YET COMPLETED
```

---

# 130. Final disposable-resource cleanup

After all evidence is captured, clean only disposable resources created by the validation work.

Potential targets include:

* test verifier containers;
* temporary PostgreSQL containers/databases;
* `openwa_meta77_validate`;
* known disposable Neon branch created specifically for validation;
* temporary admin DB if confirmed disposable.

Do not touch:

* active OpenWA production-development stack;
* preserved Neon validation database;
* InvestNaija;
* SocialForge;
* unrelated images;
* unrelated volumes.

---

# 131. Documentation that still needs to be reconciled in the repository

Once verification is complete, update:

```text
PROJECT_STATUS_HANDOVER.md
```

with final gate results.

Update:

```text
docs/superpowers/plans/2026-08-11-meta-cloud-provider.md
```

so checkboxes reflect actual evidence.

Update:

```text
docs/program/BACKEND_ADVERSARIAL_FINDINGS.md
```

with the validated Grok council findings and their final regressions.

Preserve reviewer evidence under:

```text
validation/ai-review
```

or its established external validation location as appropriate.

---

# 132. Findings that should explicitly appear in adversarial-findings documentation

At minimum document:

```text
HTTP 408 ambiguous outcome

Malformed Meta webhook item isolation

Template sync sender-health mutation

Stale template sync generation

Meta sender/provider route coherence

PostgreSQL CHECK NULL semantics

Sender-scoped Meta wamid resolution

Control-api schema readiness

PostgreSQL lock-order 40P01

Meta sender identity immutability

Memory phone-number uniqueness

Provider error-detail leakage
```

Also record the important rejected reviewer claims and the evidence for rejection.

---

# 133. Definition of local backend completion

The Meta/backend workstream should only be called locally complete when the final candidate satisfies all applicable items below:

```text
[ ] all implementation present

[ ] Baileys/WWebJS/Meta remain three independent sibling transports

[ ] canonical OpenWA content remains authoritative

[ ] all seven transport subsets work in the provider model

[ ] provider health governs eligibility

[ ] Meta sender governance complete

[ ] Meta identity immutability enforced

[ ] Meta credential resolution secure

[ ] Meta Graph client complete

[ ] ambiguous outcomes fail safe

[ ] Meta template catalogue complete

[ ] template generation concurrency protected

[ ] Meta bindings complete

[ ] transport-neutral dispatch complete

[ ] webhook signature verification complete

[ ] webhook batch isolation complete

[ ] sender-scoped delivery resolution complete

[ ] inbound processing complete

[ ] STOP integration complete

[ ] route coherence enforced in application

[ ] route coherence enforced in PostgreSQL

[ ] migrations through 0077 complete

[ ] PostgreSQL 17 zero-to-77 migration replay green

[ ] final full Go suite green

[ ] PostgreSQL opt-in suite green

[ ] go vet green

[ ] go build green

[ ] race gates green

[ ] production compose/security checks green

[ ] committed-secret scan green

[ ] OpenAPI parity green

[ ] git diff --check green

[ ] active OpenWA stack healthy

[ ] unrelated Docker projects untouched

[ ] final Grok review adjudicated

[ ] Qwen/GLM calibration review completed

[ ] any resulting fixes reverified

[ ] documentation reconciled
```

At the present handover point, the unchecked verification/reviewer items mean **formal local completion has not yet been declared**.

---

# 134. Definition of production acceptance

Production acceptance is a later milestone and requires genuine external evidence, including:

```text
Baileys real authentication

WWebJS real authentication

Meta real WABA/phone credentials

real sends across transports

real media

real inbound messages

real STOP

delivery/read acknowledgement

signed Meta webhooks

real Meta templates

ambiguous outcome behaviour

provider reconnect/recovery

endurance

target load

resource profiling

deployment/network proof

backups

restore

RPO/RTO

monitoring

runbooks

independent security assessment

frontend

UAT

accessibility
```

Local backend completion must never be presented as equivalent to production acceptance.

---

# 135. Current project state in one sentence

**The OpenWA backend is now an advanced three-transport campaign platform in which Baileys, WWebJS, and Meta Cloud API operate as independent sibling providers behind one canonical campaign/orchestration layer; the Meta backend implementation and first adversarial hardening cycle are substantially complete, PostgreSQL 17 migrations through 0077 have passed twice from zero, PG18 compatibility has been separately demonstrated for the new migrations, and the principal remaining work is recovering the interrupted full-suite evidence, completing fresh final verification, rerunning the hardened candidate through the reviewer fleet, reconciling documentation, and then proceeding separately to real/live production acceptance.**

---

# 136. What the next agent must NOT do

The next session must **not**:

```text
restart Task 6

redesign Meta from scratch

make Meta downstream of Baileys

make Meta downstream of WWebJS

turn Meta templates into the canonical campaign message

remove Baileys free-form messaging

remove WWebJS free-form messaging

assume Meta UNKNOWN is retryable across providers

rerun the full suite before attempting to recover openwa-meta77-full-go

git reset the worktree

git clean the worktree

git stash the worktree

delete unrelated Docker projects

claim full Neon 77/77 from zero

claim production acceptance

commit

push

deploy
```

without evidence or explicit authorisation where applicable.

---

# 137. What the next agent SHOULD understand immediately

The platform is already well beyond the stage of “add Meta support”.

The architecture now exists.

The important next question is not:

> “How should we implement Meta?”

It is:

> “Does the hardened three-transport backend pass every final gate, and are there any remaining defects revealed by fresh verification and independent review?”

That distinction is essential to avoid rework.

---

# 138. Immediate continuation checkpoint

The exact practical continuation point is:

```text
1. Connect Desktop Commander.

2. Inspect repository without mutation.

3. Recover:
   openwa-meta77-full-go

4. Determine its actual exit status/log.

5. Fix only an actual reproduced failure.

6. Run fresh:
   vet
   build
   race
   compose/security
   secret checks
   OpenAPI
   diff check

7. Verify OpenWA runtime stack and unrelated project preservation.

8. Run final 4-instance Grok 4.6 hardened-candidate council.

9. Run identical Qwen 3.8 Max / GLM 5.2 calibration pair.

10. Adjudicate findings.

11. Reverify if anything changes.

12. Complete Neon evidence/cleanup truthfully.

13. Reconcile plan + findings + handover.

14. Stop before commit/push/deploy unless separately authorised.
```

---

# 139. Current confidence boundary

There is substantial evidence that the backend implementation is in a strong state.

However, engineering discipline requires us to distinguish that from the final assertion:

```text
ALL FINAL LOCAL GATES GREEN
```

We have **not yet made that assertion**, because the long `openwa-meta77-full-go` result could not be recovered after Desktop Commander became unavailable and the later final gates have not yet all been rerun.

That is the correct place to resume.

---

# 140. Final architectural reference

The transport structure to preserve in every future document is:

```text
                            OPENWA
                              |
                    Campaign Orchestrator
                              |
                    Canonical Message
                              |
                     Personalisation
                              |
                           Routing
                              |
          +-------------------+-------------------+
          |                   |                   |
          v                   v                   v
       BAILEYS              WWEBJS               META
          |                   |                   |
          |                   |             Meta Adapter
          |                   |                   |
          |                   |          +--------+--------+
          |                   |          |                 |
          |                   |          v                 v
          |                   |      free-form         template
          |                   |     if permitted        binding
          |                   |          |                 |
          v                   v          +--------+--------+
      WhatsApp            WhatsApp                |
                                                  v
                                      WhatsApp Cloud API
```

There is **no** architectural line:

```text
Baileys -> Meta
```

and there is **no** architectural line:

```text
WWebJS -> Meta
```

All three are first-class provider siblings.

---

# END OF HANDOVER

**OpenWA Backend Production Engineering / Task 6 / Meta Cloud Provider Extension**
**Status as of 13 August 2026**

---

This is the version I would use as the new handover. It corrects the transport architecture and, more importantly, it does not blur the distinction between work we **implemented**, work for which we have **actual green evidence**, gates that were green **before later code changes**, and the final evidence that is still **outstanding**.

One additional point I would preserve when you save it: the handover is intentionally conservative around `openwa-meta77-full-go`. We have substantial green evidence around the hardened implementation, but until we recover that container's actual exit code/log, it remains **UNKNOWN**. That prevents a new thread from accidentally claiming we completed a test whose final output we never read.
