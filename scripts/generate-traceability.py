#!/usr/bin/env python3
import json
from collections import Counter
from pathlib import Path

root = Path(__file__).resolve().parents[1]
requirements = json.loads((root / "docs/requirements/srs_requirements.json").read_text())
evidence = json.loads((root / "docs/requirements/evidence.json").read_text())
allowed = {"IMPLEMENTED_TESTED", "PARTIAL", "NOT_STARTED", "BLOCKED_EXTERNAL"}
rows = []
for item in requirements:
    rid = item["id"]
    record = evidence.get(rid, {"status": "NOT_STARTED", "evidence": []})
    if record["status"] not in allowed:
        raise SystemExit(f"invalid status for {rid}: {record['status']}")
    rows.append({
        "id": rid,
        "requirement": item["cells"][1],
        "priority": item["cells"][2] if len(item["cells"]) > 2 else "Must",
        "acceptance": item["cells"][3] if len(item["cells"]) > 3 else "See acceptance criteria and linked tests.",
        "status": record["status"],
        "evidence": record.get("evidence", []),
        "notes": record.get("notes", ""),
    })
counts = Counter(row["status"] for row in rows)
lines = [
    "# SRS Requirement Traceability Matrix",
    "",
    "> Generated from the authoritative SRS requirement catalogue. A requirement is only marked IMPLEMENTED_TESTED when code and automated evidence exist; deployment or external-control requirements remain partial until environment evidence is available.",
    "",
    f"Total requirements: **{len(rows)}**",
    "",
    "| Status | Count |",
    "|---|---:|",
]
for status in ["IMPLEMENTED_TESTED", "PARTIAL", "BLOCKED_EXTERNAL", "NOT_STARTED"]:
    lines.append(f"| {status} | {counts.get(status, 0)} |")
lines += ["", "| ID | Priority | Status | Requirement | Evidence / notes |", "|---|---|---|---|---|"]
for row in rows:
    evidence_text = ", ".join(f"`{v}`" for v in row["evidence"])
    if row["notes"]:
        evidence_text += (" — " if evidence_text else "") + row["notes"]
    requirement = row["requirement"].replace("|", "\\|")
    evidence_text = evidence_text.replace("|", "\\|")
    lines.append(f"| {row['id']} | {row['priority']} | {row['status']} | {requirement} | {evidence_text} |")
(root / "docs/requirements/TRACEABILITY.md").write_text("\n".join(lines) + "\n")
(root / "docs/requirements/traceability.json").write_text(json.dumps(rows, indent=2) + "\n")
print(json.dumps({"total": len(rows), "counts": counts}, default=dict))
