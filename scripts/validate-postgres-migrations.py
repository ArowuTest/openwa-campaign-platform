#!/usr/bin/env python3
"""Execute the ordered PostgreSQL migration chain using the native libpq client.

This validation utility deliberately receives DATABASE_URL only through the
environment and never prints it. It is intended for isolated disposable
branches such as Neon branches or local test databases.
"""
from __future__ import annotations

import argparse
import ctypes
import os
import pathlib
import sys
import time

ROOT = pathlib.Path(__file__).resolve().parents[1]
MIGRATIONS = ROOT / "database" / "migrations"

CONNECTION_OK = 0
PGRES_COMMAND_OK = 1
PGRES_TUPLES_OK = 2


class LibPQ:
    def __init__(self) -> None:
        self.lib = ctypes.cdll.LoadLibrary("libpq.so")
        self.lib.PQconnectdb.argtypes = [ctypes.c_char_p]
        self.lib.PQconnectdb.restype = ctypes.c_void_p
        self.lib.PQstatus.argtypes = [ctypes.c_void_p]
        self.lib.PQstatus.restype = ctypes.c_int
        self.lib.PQerrorMessage.argtypes = [ctypes.c_void_p]
        self.lib.PQerrorMessage.restype = ctypes.c_char_p
        self.lib.PQexec.argtypes = [ctypes.c_void_p, ctypes.c_char_p]
        self.lib.PQexec.restype = ctypes.c_void_p
        self.lib.PQresultStatus.argtypes = [ctypes.c_void_p]
        self.lib.PQresultStatus.restype = ctypes.c_int
        self.lib.PQresultErrorMessage.argtypes = [ctypes.c_void_p]
        self.lib.PQresultErrorMessage.restype = ctypes.c_char_p
        self.lib.PQntuples.argtypes = [ctypes.c_void_p]
        self.lib.PQntuples.restype = ctypes.c_int
        self.lib.PQnfields.argtypes = [ctypes.c_void_p]
        self.lib.PQnfields.restype = ctypes.c_int
        self.lib.PQgetvalue.argtypes = [ctypes.c_void_p, ctypes.c_int, ctypes.c_int]
        self.lib.PQgetvalue.restype = ctypes.c_char_p
        self.lib.PQclear.argtypes = [ctypes.c_void_p]
        self.lib.PQfinish.argtypes = [ctypes.c_void_p]

    def connect(self, dsn: str) -> ctypes.c_void_p:
        connection = self.lib.PQconnectdb(dsn.encode())
        if not connection or self.lib.PQstatus(connection) != CONNECTION_OK:
            detail = self.error(connection)
            if connection:
                self.lib.PQfinish(connection)
            raise RuntimeError(f"PostgreSQL connection failed: {detail}")
        return connection

    def error(self, connection: ctypes.c_void_p) -> str:
        raw = self.lib.PQerrorMessage(connection) if connection else None
        return (raw.decode(errors="replace") if raw else "unknown libpq error").strip()

    def exec(self, connection: ctypes.c_void_p, sql: str, expect_rows: bool = False) -> list[list[str]]:
        result = self.lib.PQexec(connection, sql.encode())
        if not result:
            raise RuntimeError(f"PostgreSQL execution failed: {self.error(connection)}")
        try:
            status = self.lib.PQresultStatus(result)
            allowed = {PGRES_COMMAND_OK, PGRES_TUPLES_OK}
            if status not in allowed:
                raw = self.lib.PQresultErrorMessage(result)
                detail = (raw.decode(errors="replace") if raw else self.error(connection)).strip()
                raise RuntimeError(detail)
            if expect_rows or status == PGRES_TUPLES_OK:
                rows = self.lib.PQntuples(result)
                fields = self.lib.PQnfields(result)
                return [[self.lib.PQgetvalue(result, row, column).decode(errors="replace") for column in range(fields)] for row in range(rows)]
            return []
        finally:
            self.lib.PQclear(result)


def ordered_migrations() -> list[pathlib.Path]:
    files = sorted(path for path in MIGRATIONS.glob("*.sql") if path.is_file())
    if not files:
        raise RuntimeError("no migration files found")
    expected = 1
    for path in files:
        prefix = path.name.split("_", 1)[0]
        if not prefix.isdigit() or int(prefix) != expected:
            raise RuntimeError(f"migration sequence gap at {path.name}; expected {expected:04d}")
        expected += 1
    return files


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--statement-timeout", default="5min")
    parser.add_argument("--lock-timeout", default="10s")
    args = parser.parse_args()
    dsn = os.environ.get("DATABASE_URL", "").strip()
    if not dsn:
        print("DATABASE_URL is required", file=sys.stderr)
        return 2
    client = LibPQ()
    connection = client.connect(dsn)
    try:
        client.exec(connection, f"SET statement_timeout = '{args.statement_timeout}'; SET lock_timeout = '{args.lock_timeout}'; SET application_name = 'campaign-platform-migration-validation';")
        identity = client.exec(connection, "SELECT current_database(), current_user, current_setting('server_version_num')", expect_rows=True)[0]
        print(f"Connected database={identity[0]} role={identity[1]} server_version_num={identity[2]}")
        migrations = ordered_migrations()
        started = time.monotonic()
        for path in migrations:
            sql = path.read_text(encoding="utf-8")
            item_started = time.monotonic()
            try:
                client.exec(connection, sql)
            except Exception as exc:
                print(f"FAILED {path.name}: {exc}", file=sys.stderr)
                return 1
            print(f"PASS {path.name} ({time.monotonic() - item_started:.3f}s)")
        checks = client.exec(connection, """
            SELECT
              (SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE')::text,
              (SELECT count(*) FROM pg_indexes WHERE schemaname='public')::text,
              (SELECT count(*) FROM information_schema.table_constraints WHERE constraint_schema='public')::text,
              to_regclass('public.campaign_recipients')::text,
              to_regclass('public.gateway_runtime_nonces')::text,
              to_regclass('public.retention_jobs')::text
        """, expect_rows=True)[0]
        if any(value == "" for value in checks[3:]):
            raise RuntimeError(f"required migrated relations are missing: {checks[3:]}")
        print(f"Migration chain complete files={len(migrations)} tables={checks[0]} indexes={checks[1]} constraints={checks[2]} duration={time.monotonic() - started:.3f}s")
        return 0
    finally:
        client.lib.PQfinish(connection)


if __name__ == "__main__":
    raise SystemExit(main())
