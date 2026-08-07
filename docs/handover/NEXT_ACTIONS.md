# Next actions

## Backend closure after 0.8.28 consolidation

The immediate objective remains: finish the backend to the maximum extent possible, verify it, and only then move into production frontend work.

The next actions must preserve existing platform functionality. Do not satisfy build or release gates by deleting required handlers, disabling tests, weakening security controls, replacing real behaviour with stubs, or silently dropping OpenWA capabilities.

1. Reconcile the complete 398-row requirements catalogue against the current `0.8.28` source and evidence. Update classifications only where implementation/test evidence genuinely supports the change.
2. Complete the adversarial Backend Release Readiness Review against the current consolidated runtime. Any Critical/Important code findings must be corrected and reverified before backend closure.
3. Perform authenticated OpenWA transport validation on the consolidated worker: session create/start, QR and/or pairing code, READY state, text/image/video/document sends, delivery/read events, inbound messages, stop/restart, reconnect and watchdog recovery.
4. Run production-like PostgreSQL/worker failure-boundary validation where not already covered by the existing Neon `0.8.28` evidence. Do not rerun destructive schema work solely for transport-only changes.
5. Produce target-volume capacity, queueing, throughput, pacing, concurrency and endurance evidence for the control plane and gateway.
6. Complete independent security assurance/penetration testing and operational key-management review.
7. Complete target-host network/deployment validation, backup/restore evidence and disaster-recovery proof.
8. Complete operational runbook, monitoring, alerting, incident and owner-approval gates.
9. When the backend closure review confirms no further code-side backend work can reasonably be completed first, begin/complete the production frontend against the existing governed API contracts.

## External/release gates retained

- authoritative requirements/evidence traceability;
- production-like PostgreSQL validation where required;
- live authenticated OpenWA transport validation;
- target-volume performance and endurance evidence;
- independent security assessment;
- target-host deployment/network policy;
- backup, restore and disaster-recovery proof;
- formal operational-owner approval;
- production frontend completion.
