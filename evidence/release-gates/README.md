# Production release-gate evidence

Production-candidate gate closure and waiver records must reference a non-empty JSON manifest beneath this directory. The record's `source_fingerprint` is the SHA-256 digest of the exact manifest bytes.

Each schema-version 1 manifest binds the evidence to one gate, one immutable release-candidate fingerprint, and the independently named owner and approver. Its `evidence` array contains one or more observed source records with a gate-governed `kind`, a gate-local `reference`, the referenced bytes' SHA-256 `source_fingerprint`, and a UTC `observed_at` field. Referenced source artifacts must be non-empty, must remain beneath the matching `RG-NNN` directory, and cannot be the manifest itself. The verifier rejects path traversal, missing or empty artifacts, digest drift, self-reference, actor drift, and gate or candidate reuse.

Normal development checks continue to report open gates without requiring candidate evidence. Production-candidate validation activates these binding rules for every `CLOSED` or `WAIVED` gate, including otherwise-valid time-bounded waiver metadata. This layered contract preserves lightweight development status reporting while preventing a production closure or waiver from relying on unbound descriptive metadata.
