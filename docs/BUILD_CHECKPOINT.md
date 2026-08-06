# Build checkpoint

Current version: **0.8.26**

Branch: `work/backend-data-privacy-exports`

This checkpoint hardens the `0.8.25` backend across secure export delivery, privacy/data-subject workflows, audience-import governance, reporting privacy, contact lifecycle, audit search and portable object storage.

Implemented code-level scope includes:

- immutable export criteria and report as-of evidence;
- maker-checker export approval, actor-bound single-use download grants, revocation, expiry and download-outcome audit;
- deterministic PDF, CSV and XLSX rendering and unbounded paged audit export generation;
- real bounded XLSX import processing with worksheet, formula, macro, external-content and archive-expansion safeguards;
- governed reusable mappings, deterministic templates, row-level issue export, safe rollback and source-file retention/deletion workers;
- privacy cases for access, portability, rectification, objection, restriction and erasure/anonymisation;
- AES-GCM privacy-package encryption under a dedicated keyring, independent approval/execution and legal holds;
- governed small-cohort report suppression with exact policy-version evidence;
- contact lifecycle transitions and immutable profile/status history;
- expanded cursor-based audit filtering;
- filesystem or S3/MinIO-compatible object storage;
- migrations `0062`, `0063` and `0064`;
- OpenAPI coverage for 232 implemented method/path pairs.

It remains pre-production. Live PostgreSQL migration execution, production-volume query plans, dependency-resolved OpenWA builds, genuine `whatsapp-web.js` and Baileys sessions, Hostinger deployment, penetration testing, backup/restore evidence, unified platform governance/retention, production observability and the frontend remain release gates.
