#!/usr/bin/env python3
"""Fail when implemented control API routes are missing or incorrectly prefixed in OpenAPI."""
from pathlib import Path
import re
import sys

root = Path(__file__).resolve().parents[1]
server = (root / "internal/platform/httpserver/server.go").read_text(encoding="utf-8")
spec = (root / "contracts/openapi/control-api.yaml").read_text(encoding="utf-8")
route_pattern = re.compile(r'mux\.Handle(?:Func)?\("(GET|POST|PUT|DELETE|PATCH) (/api/v1/[^" ]+)"')
implemented = {(method.lower(), path[len('/api/v1'):]) for method, path in route_pattern.findall(server)}
path_blocks = {}
current = None
for line in spec.splitlines():
    match = re.match(r"^  (/[^:]+):\s*$", line)
    if match:
        current = match.group(1)
        path_blocks.setdefault(current, set())
        continue
    method = re.match(r"^    (get|post|put|delete|patch):\s*$", line)
    if current and method:
        path_blocks[current].add(method.group(1))

documented = {(method, path) for path, methods in path_blocks.items() for method in methods}
missing = sorted(implemented - documented)
invalid_prefixes = sorted(path for path in path_blocks if path.startswith('/api/v1/'))
if missing or invalid_prefixes:
    if missing:
        print("OpenAPI is missing implemented routes:", file=sys.stderr)
        for method, path in missing:
            print(f"  {method.upper()} {path}", file=sys.stderr)
    if invalid_prefixes:
        print("OpenAPI paths duplicate the server /api/v1 prefix:", file=sys.stderr)
        for path in invalid_prefixes:
            print(f"  {path}", file=sys.stderr)
    raise SystemExit(1)
print(f"OpenAPI covers {len(implemented)} implemented /api/v1 method/path pairs with no duplicate prefixes.")
