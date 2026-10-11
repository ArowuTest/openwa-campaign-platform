# Railway admin-web deployment and live-provider UAT runbook

This runbook prepares execution from accepted local canonical source. It is **not** evidence that a push, Railway change, live WhatsApp test, or production release has occurred.

## Accepted local boundary

- Canonical local branch: `work/backend-production-engineering`
- Accepted source/evidence head: `daa9f3ddede0ecc5b7d6a52e579fba140a0c5e8d`
- Application source commit: `f264237829e1caf4b0bac06c3aacd961885fc7c8`
- Current Railway control-api source observed before deployment preparation: `cc125fbfacfd171d179440a780fc4062a8c54c0a`
- Railway production project: `openwa-prod`
- Production environment ID: `546fd711-cf80-4402-89b0-cb3ae5671fa9`

The admin frontend and the backend pilot/auth contracts must move together. Do **not** deploy admin-web against the old backend source.

## Hard stop conditions

Stop before staging any Railway mutation if any of these is true:

- the accepted commit is not available in the connected GitHub repository;
- the local accepted branch is dirty or no longer matches the recorded commit;
- Railway has actionable staged changes that are not part of the approved packet;
- the seven existing application/control services cannot all be pinned to the accepted commit;
- current Railway proxy/network trust cannot be reconciled;
- deployment authorization has not been explicitly granted;
- any secret, full MSISDN, QR material or provider credential would need to be recorded in source/evidence.

## Phase A — read-only reconciliation

1. Verify GitHub contains the accepted commit.
2. Read Railway production service inventory and confirm these seven services still exist:
   - `control-api`
   - `audience-worker`
   - `campaign-worker`
   - `export-worker`
   - `inbound-governance-worker`
   - `metrics-worker`
   - `platform-governance-worker`
3. Confirm Postgres/Redis/ClamAV managed dependencies are healthy.
4. Confirm `admin-web` still does not exist before the staged create operation.
5. Confirm `get_staged_changes` contains no actionable unrelated change.
6. Reconcile proxy/network trust. Railway-marked requests must use `X-Railway-Edge` plus validated `X-Real-IP`; `X-Forwarded-For` is ignored for Railway-marked requests.
7. Run:
   - `python3 scripts/verify-admin-web-deployment-packet.py`
   - `python3 scripts/verify-admin-web-deployment-packet.py --verify-git-lineage`
   - `python3 scripts/verify-admin-web-deployment-packet.py --verify-git-lineage --ready`

The final command must remain RED until remote source visibility, explicit deploy authorization and `READY_TO_STAGE` state have all been recorded.

## Phase B — stage one integrated Railway change set

Use the exact operation order in `infrastructure/railway/admin-web-deployment-execution-2026-10-06.json`.

### Existing backend/control services

For each of the seven existing application/control services, stage `connect_service_source` using:

- repo: `ArowuTest/openwa-campaign-platform`
- branch: `work/backend-production-engineering`
- commit SHA: `daa9f3ddede0ecc5b7d6a52e579fba140a0c5e8d`
- production environment
- `staged: true`

Do not touch Postgres, Redis or ClamAV source/config in this change set.

### New admin-web service

Stage, in order:

1. `create_service`
   - name: `admin-web`
   - production environment
   - no image
   - `staged: true`
2. `update_service`
   - Dockerfile: `infrastructure/docker/admin-web.Dockerfile`
   - healthcheck: `/healthz`
   - private network endpoint: `admin-web`
   - `staged: true`
3. `set_variables`
   - only `ADMIN_WEB_CONTROL_API_URL=http://${{control-api.RAILWAY_PRIVATE_DOMAIN}}:${{control-api.PORT}}`
   - no database/provider/gateway/MSISDN credential classes
   - `staged: true`
4. `connect_service_source`
   - same repo/branch/commit as the backend services
   - `staged: true`

Inspect the staged patch and require exact equivalence with the packet before any commit.

## Phase C — explicit deploy authorization

`accept_deploy` is a separate destructive step. Do not call it until explicit user authorization is present for this exact staged packet.

After acceptance:

1. poll the environment until all seven backend/control services and admin-web are settled SUCCESS;
2. confirm the deployed backend source is the accepted commit;
3. confirm `admin-web /healthz` returns 200;
4. generate/attach the frontend domain only after the service is healthy; set the domain target port to the service's effective Railway `PORT` value rather than the Dockerfile's fallback port (production observation on 2026-10-06: `PORT=8080`);
5. run authenticated browser smoke/accessibility against the real Railway frontend:
   - `/`
   - `/campaigns`
   - `/senders`
   - `/operations`
   - `/reports`
   - `/healthz`
6. verify same-origin `/api/*` reaches private control-api and preserves session/CSRF behavior;
7. verify the Railway client-IP trust path with `X-Real-IP`.

Any failed health/auth/network check stops progression to provider UAT.

## Phase D — governed real-provider UAT

Never record QR payloads, credentials or full MSISDNs. Use internal IDs, masked numbers and evidence references only.

### 1. Organisation and consent

- Create/confirm the approved organisation record.
- Record the approved offline consent-review evidence for the Day-1 controlled operation.
- Create the approved test-recipient record; evidence must expose only the masked MSISDN.

### 2. Sender/session readiness

- Select one Day-1 engine: `BAILEYS` **or** `WHATSAPP_WEB_JS`.
- Pair the real test session without persisting QR material.
- Confirm session status `READY`, current heartbeat and exact sender/gateway pool binding.
- Record pairing and READY evidence references.

### 3. Controlled test and capacity hold

- Create the campaign with the exact engine-specific sender pool.
- Approve/freeze the message version.
- Run a controlled test to the approved test recipient.
- Record server status `ACCEPTED` on the exact campaign route.
- Create the single-route routing plan and authoritative HELD reservation.
- Final approval must succeed through the server-side pilot gate.

### 4. Live text and media proof

For both text and media:

- provider accepted;
- sent;
- delivered;
- read;
- durable evidence reference.

Do not substitute a fake-session or adapter-only probe for this evidence.

### 5. Inbound proof

- Reply from the test handset.
- Confirm inbound reply ingestion and durable evidence.
- Confirm no raw sensitive content is retained beyond policy.

### 6. Reconnect proof

- Observe a controlled disconnect.
- Reconnect the same governed session.
- Confirm it returns to `READY` with current runtime authority.
- Record evidence; do not silently reroute to a different engine.

### 7. UNKNOWN semantics

Do not intentionally create unsafe duplicate-send conditions merely to produce an UNKNOWN.

If a real UNKNOWN occurs:

- keep it sticky;
- reconcile with provider/session evidence;
- record whether provider submission occurred;
- do not resend unless `CONFIRM_NOT_SUBMITTED` is evidence-backed and duplicate risk is explicitly accepted;
- record that no duplicate send was observed.

If no safe UNKNOWN event occurs, core UAT may still complete, but production release certification remains open.

## Phase E — evidence validation

Copy `evidence/templates/openwa-live-provider-uat-template.json` to a new external-evidence file and fill only non-sensitive references.

Run:

```sh
python3 scripts/verify-live-provider-uat-evidence.py <evidence-file>
```

Core UAT can pass without claiming full release certification. A release-certification claim is rejected unless the evidence also contains:

- reconciled UNKNOWN outcome evidence;
- backup/restore PASS evidence;
- monitoring PASS evidence;
- capacity PASS evidence;
- security PASS evidence;
- rollback PASS evidence.

## Rollback

- Stop new campaign admission before rollback.
- Preserve UNKNOWN/outbox/session evidence.
- Never blind-resend ambiguous submissions.
- Roll back only to a previously accepted immutable source/deployment.
- admin-web can be removed/rolled back without changing the control-api cross-provider authority.
- Re-run backend health and browser smoke before restoring operations.
