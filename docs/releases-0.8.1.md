# 0.8.1 — Durable export rendering

This release completes the physical rendering side of the 0.8 reporting milestone. Approved campaign-report and audit exports are claimed by a fenced worker, rendered as JSON, CSV, PDF or XLSX, stored through the object-store boundary, checksummed, expired and deleted after their governed availability window. Maker-checker approval no longer marks an export ready before a file exists.

Live object-store and PostgreSQL execution remain deployment validation items.
