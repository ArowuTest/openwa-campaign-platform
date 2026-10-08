#!/usr/bin/env python3
"""Bounded accepted-source OCI packaging. No registry or production operations."""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import subprocess
import sys
import tarfile
import time
import uuid

ACCEPTED_SHA = "bfb4bee1d03105e14e97577d279e5bc5632266fe"
ACCEPTED_TREE = "62dbb4293fb61169d71d0f6f1c570cbbbbfa76f0"
REPOSITORY = "ArowuTest/openwa-campaign-platform"
SOURCE_URL = "https://github.com/" + REPOSITORY
TOOLING_REF = "refs/heads/feat/campaign-hosted-packaging-20261008"
WORKFLOW_PATH = ".github/workflows/campaign-hosted-packaging.yml"
SERVICES = ("admin-web", "control-api", "audience-worker", "campaign-worker",
            "export-worker", "inbound-governance-worker", "metrics-worker",
            "platform-governance-worker")
INPUTS = {
    "admin": ("apps/admin-web", "infrastructure/docker/admin-web.Dockerfile", ".dockerignore"),
    "backend": ("go.mod", "cmd", "internal", "infrastructure/docker", ".dockerignore"),
}
DIGEST = re.compile(r"sha256:[0-9a-f]{64}\Z")
HEX40 = re.compile(r"[0-9a-f]{40}\Z")
CONTAINER_ID = re.compile(r"[0-9a-f]{64}\Z")


class GateFailure(Exception):
    pass


def require(condition, code):
    if not condition:
        raise GateFailure(code)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True).encode("ascii")


def sha_bytes(value):
    return hashlib.sha256(value).hexdigest()


def sha_file(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def stamp():
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())


def write_json(path, value):
    path = Path(path)
    require(not path.exists(), "RECEIPT_OVERWRITE_REFUSED")
    path.write_bytes(canonical(value) + b"\n")
    return {"path": path.name, "sha256": sha_file(path)}


def plain_builder(text, expected="default"):
    sections = re.split(r"(?m)^Nodes:[ \t]*\r?$", text.rstrip("\r\n"))
    require(len(sections) == 2, "BUILDER_INSPECTION_SHAPE")
    values = {}
    for key, field, section in (
        ("name", "Name", sections[0]), ("driver", "Driver", sections[0]),
        ("nodeName", "Name", sections[1]), ("endpoint", "Endpoint", sections[1]),
        ("status", "Status", sections[1]),
    ):
        matches = re.findall(r"(?m)^" + field + r":[ \t]*([^\r\n]+?)[ \t]*\r?$", section)
        require(len(matches) == 1, "BUILDER_INSPECTION_SHAPE")
        values[key] = matches[0].strip()
    require(values == {"name": expected, "driver": "docker", "nodeName": expected,
                       "endpoint": expected, "status": "running"}, "BUILDER_BINDING")
    return values


def safe_archive_name(name):
    p = PurePosixPath(name)
    require(name and not p.is_absolute() and "\\" not in name
            and ".." not in p.parts and "." not in p.parts
            and ":" not in name and not any(ord(c) < 32 for c in name), "ARCHIVE_PATH_REFUSED")
    return p


def secret_path(name):
    p = PurePosixPath(name)
    return any((part == ".env" or part.startswith(".env.")) and part != ".env.example"
               for part in p.parts) or p.suffix.lower() in (".pem", ".key", ".pfx", ".p12")


def approved_path(name, kind):
    return any(name == p or name.startswith(p + "/") for p in INPUTS[kind])


def validate_member(member, kind):
    p = safe_archive_name(member.name.rstrip("/"))
    name = p.as_posix()
    require(approved_path(name, kind) or member.isdir() and any(p.startswith(name + "/") for p in INPUTS[kind]),
            "UNAPPROVED_ARCHIVE_INPUT")
    require(not secret_path(name), "SECRET_CLASS_INPUT_REFUSED")
    require(member.isdir() or member.isfile(), "LINK_OR_SPECIAL_ARCHIVE_INPUT_REFUSED")
    require(member.size >= 0, "INVALID_ARCHIVE_SIZE")
    return name


def input_aggregate(rows):
    return sha_bytes("\n".join(sorted(row["path"] + "\t" + row["sha256"] for row in rows)).encode())


def scan_context(context):
    rows = []
    for p in sorted(Path(context).rglob("*")):
        require(not p.is_symlink(), "CONTEXT_SYMLINK_REFUSED")
        require(p.is_file() or p.is_dir(), "CONTEXT_SPECIAL_INPUT_REFUSED")
        if p.is_file():
            name = p.relative_to(context).as_posix()
            safe_archive_name(name)
            require(not secret_path(name), "SECRET_CLASS_INPUT_REFUSED")
            rows.append({"path": name, "bytes": p.stat().st_size, "sha256": sha_file(p)})
    require(rows, "EMPTY_CONTEXT")
    return sorted(rows, key=lambda row: row["path"])


def metadata_identity(metadata, iid, image):
    manifest = metadata.get("containerimage.digest")
    if "containerimage.descriptor" not in metadata:
        config = metadata.get("containerimage.config.digest")
        require(isinstance(manifest, str) and DIGEST.fullmatch(manifest)
                and isinstance(config, str) and DIGEST.fullmatch(config)
                and config == manifest and iid == manifest and image.get("id") == manifest,
                "METADATA_CONFIG_EXPORT_BINDING")
        return {"manifestDigest": manifest, "descriptorDigest": None, "configDigest": config,
                "iid": iid, "identityScope": "ENGINE_ID_MATCHES_METADATA_CONFIG_DIGEST_NO_DESCRIPTOR"}
    descriptor = metadata.get("containerimage.descriptor", {})
    require(isinstance(manifest, str) and DIGEST.fullmatch(manifest)
            and isinstance(descriptor, dict) and descriptor.get("digest") == manifest,
            "METADATA_MANIFEST_BINDING")
    config = metadata.get("containerimage.config.digest")
    require(config is None or isinstance(config, str) and DIGEST.fullmatch(config), "CONFIG_DIGEST_INVALID")
    require(isinstance(image.get("id"), str) and DIGEST.fullmatch(image["id"]), "IMAGE_ID_INVALID")
    require(DIGEST.fullmatch(iid) and iid in (manifest, config, image["id"]), "IID_BINDING")
    if config:
        require(image["id"] in (manifest, config), "IMAGE_METADATA_DISAGREEMENT")
    # The same genuine Dockerfiles may expose manifest vs config IDs differently.
    scope = "ENGINE_ID_RECORDED_SEPARATELY"
    if config and image["id"] == config:
        scope = "ENGINE_ID_MATCHES_METADATA_CONFIG_DIGEST"
    elif image["id"] == manifest:
        scope = "ENGINE_ID_MATCHES_METADATA_MANIFEST_DIGEST"
    return {"manifestDigest": manifest, "descriptorDigest": descriptor["digest"],
            "configDigest": config, "iid": iid, "identityScope": scope}


def validate_image(image, service):
    require(image["os"] == "linux" and image["architecture"] == "amd64"
            and image["revision"] == ACCEPTED_SHA and image["source"] == SOURCE_URL, "IMAGE_SOURCE_PLATFORM")
    if service == "admin-web":
        require(image["user"] == "campaign" and image["cmd"] == ["node", "server.js"], "ADMIN_RUNTIME")
    else:
        require(image["user"] == "campaign:campaign" and image["entrypoint"] == ["/" + service], "BACKEND_RUNTIME")


def image_essentials(raw):
    config = raw.get("Config") or {}
    labels = config.get("Labels") or {}
    return {"id": raw.get("Id"), "os": raw.get("Os"), "architecture": raw.get("Architecture"),
            "user": config.get("User"), "cmd": config.get("Cmd"), "entrypoint": config.get("Entrypoint"),
            "revision": labels.get("org.opencontainers.image.revision"),
            "source": labels.get("org.opencontainers.image.source")}


def public_image_env(image_env):
    allowed = {"PATH": r"/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
               "NODE_VERSION": r"22\.19\.[0-9]+", "YARN_VERSION": r"[0-9]+\.[0-9]+\.[0-9]+",
               "NODE_ENV": r"production", "HOSTNAME": r"0\.0\.0\.0", "PORT": r"3000"}
    values = {}
    for entry in image_env:
        require(isinstance(entry, str) and "=" in entry, "IMAGE_ENV_SHAPE")
        key, value = entry.split("=", 1)
        require(key in allowed and key not in values and re.fullmatch(allowed[key], value),
                "UNEXPECTED_IMAGE_ENV_REFUSED")
        values[key] = value
    require(set(values) == set(allowed), "IMAGE_ENV_FIELD_SET")
    return sorted(k + "=" + v for k, v in values.items())


def expected_env(image_env):
    result = {}
    for item in public_image_env(image_env):
        require(isinstance(item, str) and "=" in item, "IMAGE_ENV_SHAPE")
        key, value = item.split("=", 1)
        require(key not in result, "DUPLICATE_IMAGE_ENV")
        result[key] = value
    result.update(PORT="8080", HOSTNAME="0.0.0.0")
    return sorted(k + "=" + v for k, v in result.items())


def isolation_proof(raw, image_id, owner, image_env):
    host = raw.get("HostConfig") or {}
    config = raw.get("Config") or {}
    state = raw.get("State") or {}
    mounts = raw.get("Mounts") or []
    tmpfs = host.get("Tmpfs") or {}
    actual_env = config.get("Env") or []
    require(isinstance(actual_env, list) and len(actual_env) == len(set(actual_env))
            and sorted(actual_env) == expected_env(image_env), "CONTAINER_ENV_BINDING")
    require(raw.get("Image") == image_id and (config.get("Labels") or {}).get("openwa.packaging.owner") == owner,
            "CONTAINER_OWNERSHIP_BINDING")
    require(host.get("NetworkMode") == "none" and host.get("ReadonlyRootfs") is True
            and host.get("Memory") == 268435456 and host.get("MemorySwap") == 268435456
            and host.get("NanoCpus") == 250000000 and host.get("PidsLimit") == 128
            and (host.get("RestartPolicy") or {}).get("Name") == "no", "CONTAINER_RESOURCE_ISOLATION")
    require(not host.get("PortBindings") and not host.get("Binds")
            and not host.get("Mounts")
            and all(m.get("Type") == "tmpfs" and m.get("Destination") == "/tmp" for m in mounts)
            and tmpfs == {"/tmp": "rw,size=64m"}, "CONTAINER_MOUNT_PORT_ISOLATION")
    require(host.get("CapDrop") == ["ALL"]
            and host.get("SecurityOpt") in (["no-new-privileges"], ["no-new-privileges=true"]),
            "CONTAINER_PRIVILEGE_ISOLATION")
    return {"id": raw["Id"], "imageId": raw["Image"], "owner": owner,
            "networkMode": host["NetworkMode"], "readonlyRootfs": host["ReadonlyRootfs"],
            "memory": host["Memory"], "memorySwap": host["MemorySwap"], "nanoCpus": host["NanoCpus"],
            "pidsLimit": host["PidsLimit"], "restartPolicy": "no", "ports": host.get("PortBindings"),
            "mounts": [{"type": m["Type"], "destination": m["Destination"]} for m in mounts],
            "tmpfs": tmpfs, "capDrop": host["CapDrop"], "securityOpt": host["SecurityOpt"],
            "envOverrides": ["HOSTNAME=0.0.0.0", "PORT=8080"], "envMatchesImagePlusExactOverrides": True,
            "actualEnv": expected_env(image_env), "imageEnv": public_image_env(image_env),
            "imageEnvSha256": sha_bytes(canonical(sorted(image_env))),
            "running": state.get("Running"), "oomKilled": state.get("OOMKilled"), "exitCode": state.get("ExitCode")}


PROBE = r'''const http=require("node:http");let done=false;
function finish(ok,code,extra={}){if(done)return;done=true;console.log(JSON.stringify({ok,code,...extra}));process.exit(ok?0:1)}
const req=http.get("http://127.0.0.1:8080/healthz",res=>{let body="";res.on("data",b=>{body+=b;if(Buffer.byteLength(body)>4096){req.destroy();finish(false,"BODY_LIMIT")}});res.on("end",()=>{try{const j=JSON.parse(body);const safe=Object.keys(j).sort().join(",")==="service,status"&&j.status==="ok"&&j.service==="admin-web";finish(res.statusCode===200&&safe,"HEALTH_RESPONSE",safe?{statusCode:res.statusCode,status:j.status,service:j.service,rawSafeBody:body}:{})}catch{finish(false,"HEALTH_JSON")}})});
req.setTimeout(2000,()=>{req.destroy();finish(false,"HTTP_TIMEOUT")});req.on("error",()=>finish(false,"HTTP_CONNECT"));setTimeout(()=>finish(false,"PROBE_DEADLINE"),2400).unref();'''


def safe_probe(stdout):
    try:
        value = json.loads(stdout)
    except (ValueError, TypeError):
        return None
    require(isinstance(value, dict) and type(value.get("ok")) is bool
            and value.get("code") in ("BODY_LIMIT", "HEALTH_RESPONSE", "HEALTH_JSON", "HTTP_TIMEOUT",
                                      "HTTP_CONNECT", "PROBE_DEADLINE"), "UNRECOGNIZED_PROBE_OUTPUT")
    if "rawSafeBody" in value:
        require(set(value) == {"ok", "code", "statusCode", "status", "service", "rawSafeBody"},
                "PROBE_FIELD_BINDING")
        body = value["rawSafeBody"]
        require(isinstance(body, str) and len(body.encode()) <= 4096
                and json.loads(body) == {"status": "ok", "service": "admin-web"}
                and value["status"] == "ok" and value["service"] == "admin-web"
                and type(value["statusCode"]) is int, "PROBE_BODY_BINDING")
    else:
        require(set(value) == {"ok", "code"} and not value["ok"], "PROBE_FAILURE_SHAPE")
    if value["ok"]:
        require(value["code"] == "HEALTH_RESPONSE" and value.get("statusCode") == 200, "HEALTH_SUCCESS_BINDING")
    return value


class Packaging:
    def __init__(self, source, tooling, base):
        self.source = Path(source).resolve()
        self.tooling = Path(tooling).resolve()
        require(self.source != self.tooling and self.source not in self.tooling.parents
                and self.tooling not in self.source.parents, "SEPARATE_CHECKOUTS_REQUIRED")
        self.attempt = uuid.uuid4().hex
        self.output = Path(base).resolve() / ("campaign-" + ACCEPTED_SHA + "-" + self.attempt)
        require(not self.output.exists(), "FRESH_OUTPUT_REQUIRED")
        require(self.source not in self.output.parents and self.tooling not in self.output.parents,
                "OUTPUT_OUTSIDE_CHECKOUTS_REQUIRED")
        self.output.mkdir(parents=True)
        self.deadline = time.monotonic() + 3000
        self.finalizing = False
        self.docker_context = "default"
        self.record = {"schemaVersion": 1, "state": "STARTING", "startedAtUTC": stamp(),
                       "attemptId": self.attempt, "acceptedSource": {"sha": ACCEPTED_SHA, "tree": ACCEPTED_TREE},
                       "workflow": {}, "contexts": {}, "builds": [], "adminHealth": None,
                       "failures": [], "runnerLocalOnly": True, "registryPublished": False,
                       "deployed": False, "productionChanged": False, "secretsRecorded": False,
                       "proofScope": "Accepted-source local Dockerfile builds and isolated admin boot health only.",
                       "limits": {"harnessSeconds": 3000, "eachBuildSeconds": 1200,
                                  "buildsSerial": True, "assertedPerBuildCpuMemoryHardCaps": False,
                                  "clientTimeoutMayLeaveDaemonBuildWork": False}}

    def run(self, args, timeout=15, cwd=None):
        remaining = self.deadline - time.monotonic() - (0 if self.finalizing else 60)
        require(remaining > 0, "HARNESS_DEADLINE")
        env = os.environ.copy()
        # Explicit local context; avoid external Docker endpoint overrides.
        for key in ("DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH"):
            env.pop(key, None)
        started = time.monotonic()
        try:
            result = subprocess.run(args, cwd=cwd, env=env, capture_output=True, timeout=min(timeout, remaining))
            out, err = result.stdout, result.stderr
            receipt = {"exitCode": result.returncode, "timedOut": False,
                       "elapsedSeconds": round(time.monotonic() - started, 3),
                       "outputSha256": sha_bytes(out + b"\n" + err), "rawOutputRetained": False}
            return receipt, out.decode("utf-8", "strict")
        except subprocess.TimeoutExpired:
            return {"exitCode": 124, "timedOut": True,
                    "elapsedSeconds": round(time.monotonic() - started, 3),
                    "outputSha256": None, "rawOutputRetained": False}, ""

    def git(self, root, *args):
        status, out = self.run(["git", "-C", str(root), *args])
        require(status["exitCode"] == 0, "GIT_OPERATION_FAILED")
        return out.strip()

    def docker(self, *args, timeout=15):
        return self.run(["docker", "--context", self.docker_context, *args], timeout)

    def docker_json(self, *args):
        result, out = self.docker(*args)
        require(result["exitCode"] == 0, "DOCKER_INSPECT_FAILED")
        try:
            return json.loads(out)
        except ValueError:
            raise GateFailure("DOCKER_JSON_INVALID") from None

    def source_state(self):
        state = {"head": self.git(self.source, "rev-parse", "HEAD"),
                 "tree": self.git(self.source, "rev-parse", "HEAD^{tree}"),
                 "clean": not bool(self.git(self.source, "status", "--porcelain=v1", "--untracked-files=all")),
                 "checkedAtUTC": stamp()}
        require(state["head"] == ACCEPTED_SHA and state["tree"] == ACCEPTED_TREE and state["clean"],
                "ACCEPTED_SOURCE_NOT_CLEAN_EXACT")
        return state

    def workflow_state(self):
        identity = {key: os.environ.get(key, "") for key in
                    ("GITHUB_REPOSITORY", "GITHUB_REF", "GITHUB_SHA", "GITHUB_RUN_ID",
                     "GITHUB_RUN_ATTEMPT", "GITHUB_JOB", "GITHUB_WORKFLOW", "GITHUB_WORKFLOW_REF")}
        require(identity["GITHUB_REPOSITORY"] == REPOSITORY and identity["GITHUB_REF"] == TOOLING_REF,
                "WORKFLOW_REPOSITORY_REF")
        head = self.git(self.tooling, "rev-parse", "HEAD")
        require(HEX40.fullmatch(head) and head == identity["GITHUB_SHA"] and head != ACCEPTED_SHA,
                "TOOLING_HEAD_BINDING")
        require(not self.git(self.tooling, "status", "--porcelain=v1", "--untracked-files=all"),
                "TOOLING_CHECKOUT_DIRTY")
        require(re.fullmatch(r"[1-9][0-9]*", identity["GITHUB_RUN_ID"])
                and re.fullmatch(r"[1-9][0-9]*", identity["GITHUB_RUN_ATTEMPT"])
                and identity["GITHUB_JOB"] == "package"
                and identity["GITHUB_WORKFLOW"] == "Campaign hosted packaging"
                and identity["GITHUB_WORKFLOW_REF"] == REPOSITORY + "/" + WORKFLOW_PATH + "@" + TOOLING_REF,
                "WORKFLOW_RUN_IDENTITY")
        script = Path(__file__).resolve()
        require(script == self.tooling / "scripts/campaign-hosted-packaging.py", "SCRIPT_PATH_BINDING")
        paths = ("scripts/campaign-hosted-packaging.py", WORKFLOW_PATH)
        hashes = {}
        for p in paths:
            require(not (self.tooling / p).is_symlink(), "TOOLING_SYMLINK_REFUSED")
            status, blob = self.run(["git", "-C", str(self.tooling), "show", head + ":" + p])
            require(status["exitCode"] == 0 and sha_file(self.tooling / p) == sha_bytes(blob.encode()),
                    "TOOLING_FILE_BLOB_BINDING")
            hashes[p] = sha_file(self.tooling / p)
        return {"head": head, "repository": REPOSITORY, "ref": TOOLING_REF,
                "runId": identity["GITHUB_RUN_ID"], "runAttempt": identity["GITHUB_RUN_ATTEMPT"],
                "jobKey": identity["GITHUB_JOB"], "workflowName": identity["GITHUB_WORKFLOW"],
                "workflowRef": identity["GITHUB_WORKFLOW_REF"], "scriptSha256": hashes[paths[0]],
                "workflowSha256": hashes[paths[1]], "numericJobAndWorkflowIdsSource": "External actual GitHub API attestation"}

    def builder(self):
        current, text = self.docker("context", "show")
        require(current["exitCode"] == 0 and text.strip() == "default", "LOCAL_CONTEXT_REQUIRED")
        endpoints = self.docker_json("context", "inspect", "default")
        require(isinstance(endpoints, list) and len(endpoints) == 1
                and endpoints[0].get("Endpoints", {}).get("docker", {}).get("Host") == "unix:///var/run/docker.sock",
                "LOCAL_UNIX_ENDPOINT_REQUIRED")
        info = self.docker_json("info", "--format", "{{json .}}")
        require(info.get("OSType") == "linux" and info.get("Architecture") in ("x86_64", "amd64")
                and type(info.get("NCPU")) is int and info["NCPU"] > 0
                and type(info.get("MemTotal")) is int and info["MemTotal"] > 0, "ENGINE_PLATFORM_RESOURCES")
        status, plain = self.docker("buildx", "inspect", "default")
        require(status["exitCode"] == 0, "BUILDER_INSPECT_FAILED")
        parsed = plain_builder(plain)
        return {"context": "default", "endpoint": "unix:///var/run/docker.sock",
                "serverVersion": info.get("ServerVersion"), "os": info["OSType"],
                "architecture": info["Architecture"], "cpus": info["NCPU"], "memoryBytes": info["MemTotal"],
                "builder": parsed, "inspectionSha256": sha_bytes(plain.encode()),
                "resourceScope": "Serial builds; no asserted hard per-build CPU/memory caps."}

    def prepare(self, kind):
        archive = self.output / (kind + "-source.tar")
        context = self.output / (kind + "-context")
        context.mkdir()
        tracked = self.git(self.source, "ls-tree", "-r", "--name-only", ACCEPTED_SHA, "--", *INPUTS[kind]).splitlines()
        require(tracked and all(approved_path(name, kind) and not secret_path(name) for name in tracked),
                "TRACKED_INPUT_POLICY")
        status, _ = self.run(["git", "-C", str(self.source), "archive", "--format=tar",
                              "--output=" + str(archive), ACCEPTED_SHA, *INPUTS[kind]], 30)
        require(status["exitCode"] == 0, "SOURCE_ARCHIVE_FAILED")
        archive_hash = sha_file(archive)
        seen = set()
        with tarfile.open(archive, "r:") as tar:
            members = tar.getmembers()
            for member in members:
                name = validate_member(member, kind)
                require(name not in seen, "DUPLICATE_ARCHIVE_PATH")
                seen.add(name)
                target = context.joinpath(*PurePosixPath(name).parts)
                require(context == target.parent or context in target.parents, "ARCHIVE_ESCAPE")
                if member.isdir():
                    target.mkdir(parents=True, exist_ok=True)
                else:
                    target.parent.mkdir(parents=True, exist_ok=True)
                    require(not target.exists(), "ARCHIVE_FILE_COLLISION")
                    stream = tar.extractfile(member)
                    require(stream is not None, "ARCHIVE_READ_FAILED")
                    with stream, target.open("xb") as out:
                        while True:
                            block = stream.read(1048576)
                            if not block:
                                break
                            out.write(block)
                    require(target.stat().st_size == member.size, "ARCHIVE_SIZE_MISMATCH")
                    target.chmod(member.mode & 0o777)
        rows = scan_context(context)
        require(set(row["path"] for row in rows) == set(tracked), "ARCHIVE_TRACKED_FILE_SET")
        manifest = write_json(self.output / (kind + "-inputs.json"), rows)
        record = {"contextPath": context.name, "archivePath": archive.name, "paths": list(INPUTS[kind]),
                  "archiveSha256": archive_hash, "inputCount": len(rows),
                  "inputManifestSha256": input_aggregate(rows), "inputManifestFile": manifest,
                  "beforeAfterUnchanged": False}
        self.record["contexts"][kind] = record
        return context

    def assert_contexts(self):
        for kind, record in self.record["contexts"].items():
            rows = scan_context(self.output / record["contextPath"])
            require(sha_file(self.output / record["archivePath"]) == record["archiveSha256"]
                    and input_aggregate(rows) == record["inputManifestSha256"]
                    and sha_file(self.output / record["inputManifestFile"]["path"]) == record["inputManifestFile"]["sha256"]
                    and rows == json.loads((self.output / record["inputManifestFile"]["path"]).read_bytes()),
                    "ARCHIVE_OR_CONTEXT_DRIFT")
            record["afterArchiveSha256"] = sha_file(self.output / record["archivePath"])
            record["afterInputManifestSha256"] = input_aggregate(rows)
            record["beforeAfterUnchanged"] = True

    def inspect_image(self, identity, service):
        result = self.docker_json("image", "inspect", identity)
        require(isinstance(result, list) and len(result) == 1, "IMAGE_INSPECTION_SHAPE")
        image = image_essentials(result[0])
        validate_image(image, service)
        return image, result[0].get("Config", {}).get("Env") or []

    def build(self, service):
        self.source_state()
        self.assert_contexts()
        kind = "admin" if service == "admin-web" else "backend"
        context = self.output / self.record["contexts"][kind]["contextPath"]
        run = uuid.uuid4().hex
        tag = "openwa-" + service + "-candidate:" + ACCEPTED_SHA + "-" + self.attempt[:8] + "-" + run[:8]
        metadata_path = self.output / ("image-" + service + "-" + run + ".json")
        iid_path = self.output / ("iid-" + service + "-" + run + ".txt")
        df = context / ("infrastructure/docker/" + service + ".Dockerfile")
        receipt = {"service": service, "runId": run, "state": "STARTING", "tag": tag,
                   "acceptedSha": ACCEPTED_SHA, "attemptId": self.attempt, "contextKind": kind,
                   "dockerfilePath": "infrastructure/docker/" + service + ".Dockerfile",
                   "dockerfileSha256": sha_file(df), "archiveSha256": self.record["contexts"][kind]["archiveSha256"],
                   "inputManifestBefore": self.record["contexts"][kind]["inputManifestSha256"],
                   "builder": self.builder(), "startedAtUTC": stamp()}
        self.record["builds"].append(receipt)
        try:
            exists, _ = self.docker("image", "inspect", tag)
            require(exists["exitCode"] != 0 and not exists["timedOut"], "UNIQUE_TAG_CHECK_FAILED")
            receipt["build"] = self.docker(
                "buildx", "build", "--builder", "default", "--load", "--provenance=false",
                "--platform", "linux/amd64", "--metadata-file", str(metadata_path), "--iidfile", str(iid_path),
                "--label", "org.opencontainers.image.revision=" + ACCEPTED_SHA,
                "--label", "org.opencontainers.image.source=" + SOURCE_URL,
                "-f", str(df), "-t", tag, str(context), timeout=1200)[0]
            if receipt["build"]["timedOut"]:
                self.record["limits"]["clientTimeoutMayLeaveDaemonBuildWork"] = True
            require(receipt["build"]["exitCode"] == 0 and not receipt["build"]["timedOut"], "IMAGE_BUILD_FAILED")
            metadata = json.loads(metadata_path.read_bytes())
            iid = iid_path.read_text().strip()
            image, _ = self.inspect_image(tag, service)
            receipt.update(metadata_identity(metadata, iid, image))
            receipt["image"] = image
            receipt["metadataFile"] = {"path": metadata_path.name, "sha256": sha_file(metadata_path)}
            receipt["iidFile"] = {"path": iid_path.name, "sha256": sha_file(iid_path)}
            receipt["localBuildReference"] = tag.split(":")[0] + "@" + receipt["manifestDigest"]
            self.assert_contexts()
            receipt["inputManifestAfter"] = self.record["contexts"][kind]["afterInputManifestSha256"]
            receipt["sourceAfter"] = self.source_state()
            receipt["state"] = "BUILT_LOCAL_ACCEPTED_SOURCE"
        except Exception:
            receipt["state"] = "FAILED"
            raise
        finally:
            receipt["finishedAtUTC"] = stamp()
            receipt["receiptFile"] = write_json(self.output / ("build-" + service + "-" + run + ".json"),
                                                {k: v for k, v in receipt.items() if k != "receiptFile"})

    def inspect_container(self, identity):
        result = self.docker_json("container", "inspect", identity)
        require(isinstance(result, list) and len(result) == 1 and CONTAINER_ID.fullmatch(result[0].get("Id", "")),
                "CONTAINER_INSPECTION_SHAPE")
        return result[0]

    def health(self, admin):
        image, image_env = self.inspect_image(admin["image"]["id"], "admin-web")
        require(image["id"] == admin["image"]["id"], "HEALTH_IMAGE_BINDING")
        require(sha_file(self.output / admin["receiptFile"]["path"]) == admin["receiptFile"]["sha256"],
                "ADMIN_BUILD_RECEIPT_DRIFT")
        owner = uuid.uuid4().hex
        name = "openwa-admin-candidate-" + owner
        identity = None
        health = {"state": "STARTING", "owner": owner, "imageId": image["id"],
                  "manifestDigest": admin["manifestDigest"], "buildReceiptFile": admin["receiptFile"],
                  "endpoint": "http://127.0.0.1:8080/healthz", "passed": False,
                  "limits": {"overallSeconds": 30, "httpTimeoutMs": 2000,
                             "eachDockerExecSeconds": 3, "bodyBytes": 4096},
                  "attempts": [], "cleanup": {"removed": False, "absenceConfirmed": False}}
        self.record["adminHealth"] = health
        try:
            create, out = self.docker(
                "container", "create", "--name", name, "--network", "none", "--memory", "256m",
                "--memory-swap", "256m", "--cpus", "0.25", "--pids-limit", "128", "--read-only",
                "--tmpfs", "/tmp:rw,size=64m", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
                "--label", "openwa.packaging.owner=" + owner, "--env", "PORT=8080",
                "--env", "HOSTNAME=0.0.0.0", image["id"])
            health["create"] = create
            require(create["exitCode"] == 0 and CONTAINER_ID.fullmatch(out.strip()), "CONTAINER_CREATE_FAILED")
            identity = out.strip()
            health["containerId"] = identity
            health["constraintsBeforeProbe"] = isolation_proof(self.inspect_container(identity), image["id"], owner, image_env)
            require(not health["constraintsBeforeProbe"]["running"], "CONTAINER_ALREADY_RUNNING")
            start, _ = self.docker("container", "start", identity)
            health["start"] = start
            require(start["exitCode"] == 0, "CONTAINER_START_FAILED")
            clock = time.monotonic()
            while time.monotonic() - clock <= 26:
                command, out = self.docker("container", "exec", identity, "node", "-e", PROBE, timeout=3)
                result = safe_probe(out.strip()) if out.strip() else None
                health["attempts"].append({"elapsedSeconds": round(time.monotonic() - clock, 3),
                                           "command": command, "result": result})
                if command["exitCode"] == 0 and not command["timedOut"] and result and result["ok"]:
                    health["passed"] = True
                    health["response"] = result
                    break
                if time.monotonic() - clock > 25.75:
                    break
                time.sleep(0.25)
            health["elapsedSeconds"] = round(time.monotonic() - clock, 3)
            require(health["elapsedSeconds"] <= 30, "HEALTH_OVERALL_BUDGET")
            health["constraintsAfterProbe"] = isolation_proof(self.inspect_container(identity), image["id"], owner, image_env)
            require(health["passed"] and health["constraintsAfterProbe"]["running"]
                    and not health["constraintsAfterProbe"]["oomKilled"], "ADMIN_HEALTH_FAILED")
            health["state"] = "ISOLATED_ADMIN_BOOT_HEALTH_PASS"
        except Exception as error:
            health["state"] = "FAILED"
            health["primaryFailureCode"] = str(error) if isinstance(error, GateFailure) else "SAFE_HEALTH_FAILURE"
            raise
        finally:
            # A create command may have succeeded before its client timed out: recover only our name/label.
            previous_finalizing = self.finalizing
            self.finalizing = True
            try:
                raw = self.inspect_container(identity or name)
                require((raw.get("Config", {}).get("Labels") or {}).get("openwa.packaging.owner") == owner
                        and raw.get("Image") == image["id"] and (identity is None or raw["Id"] == identity),
                        "CLEANUP_OWNERSHIP_BINDING")
                identity = raw["Id"]
                removed, _ = self.docker("container", "rm", "--force", identity)
                absent, ids = self.docker("container", "ls", "--all", "--no-trunc", "--filter", "id=" + identity,
                                         "--format", "{{.ID}}")
                health["cleanup"] = {"containerId": identity, "remove": removed, "absenceCheck": absent,
                                     "removed": removed["exitCode"] == 0,
                                     "absenceConfirmed": absent["exitCode"] == 0 and not ids.strip()}
                require(health["cleanup"]["removed"] and health["cleanup"]["absenceConfirmed"], "OWN_CONTAINER_CLEANUP_FAILED")
            except Exception:
                health["state"] = "FAILED"
                health["passed"] = False
                health["cleanup"]["failureCode"] = "OWN_CONTAINER_CLEANUP_NOT_PROVEN"
                raise GateFailure("OWN_CONTAINER_CLEANUP_NOT_PROVEN") from None
            finally:
                self.finalizing = previous_finalizing
                health["receiptFile"] = write_json(self.output / ("admin-health-" + owner + ".json"),
                                                   {k: v for k, v in health.items() if k != "receiptFile"})

    def execute(self):
        exit_code = 1
        try:
            self.record["workflow"] = self.workflow_state()
            self.record["acceptedSource"]["before"] = self.source_state()
            self.record["initialBuilder"] = self.builder()
            for kind in INPUTS:
                self.prepare(kind)
            self.assert_contexts()
            for service in SERVICES:
                self.build(service)
            self.health(self.record["builds"][0])
            self.assert_contexts()
            self.record["acceptedSource"]["after"] = self.source_state()
            require(len(self.record["builds"]) == 8
                    and {b["service"] for b in self.record["builds"]} == set(SERVICES)
                    and all(b["state"] == "BUILT_LOCAL_ACCEPTED_SOURCE" for b in self.record["builds"])
                    and self.record["adminHealth"]["passed"]
                    and self.record["adminHealth"]["cleanup"]["removed"]
                    and self.record["adminHealth"]["cleanup"]["absenceConfirmed"], "COMPLETION_CONTRACT")
            self.record["state"] = "PASSED"
            exit_code = 0
        except Exception as error:
            self.record["state"] = "FAILED"
            code = str(error) if isinstance(error, GateFailure) else "SAFE_HARNESS_FAILURE"
            self.record["failures"].append({"code": code, "type": type(error).__name__})
        finally:
            self.finalizing = True
            try:
                self.record["acceptedSource"]["after"] = self.source_state()
                self.assert_contexts()
            except Exception:
                self.record["state"] = "FAILED"
                self.record["failures"].append({"code": "FINAL_SOURCE_OR_CONTEXT_CHECK_FAILED"})
                exit_code = 1
            self.record["finishedAtUTC"] = stamp()
            self.record["acceptedSource"]["unchanged"] = (
                self.record["acceptedSource"].get("before", {}).get("head") == ACCEPTED_SHA
                and self.record["acceptedSource"].get("after", {}).get("head") == ACCEPTED_SHA
                and self.record["acceptedSource"].get("before", {}).get("tree") == ACCEPTED_TREE
                and self.record["acceptedSource"].get("after", {}).get("tree") == ACCEPTED_TREE
                and self.record["acceptedSource"].get("before", {}).get("clean") is True
                and self.record["acceptedSource"].get("after", {}).get("clean") is True)
            if not self.record["acceptedSource"]["unchanged"]:
                self.record["state"] = "FAILED"
                exit_code = 1
            write_json(self.output / "proof.json", self.record)
            emit_proof(self.record)
        return exit_code


def emit_proof(proof):
    payload = canonical(proof)
    encoded = base64.b64encode(payload).decode("ascii")
    digest = sha_bytes(payload)
    if len(encoded) <= 16000:
        print("CAMPAIGN_HOSTED_PROOF_V1:" + digest + ":" + encoded, flush=True)
    else:
        chunks = [encoded[i:i + 16000] for i in range(0, len(encoded), 16000)]
        for index, chunk in enumerate(chunks, 1):
            print("CAMPAIGN_HOSTED_PROOF_V1:" + digest + ":" + str(index) + ":" + str(len(chunks)) + ":" + chunk, flush=True)


def self_test():
    checks = 0

    def reject(fn):
        nonlocal checks
        try:
            fn()
        except GateFailure:
            checks += 1
        else:
            raise AssertionError("Expected fail-closed validation")

    valid = "Name: default\nDriver: docker\nNodes:\nName: default\nEndpoint: default\nStatus: running\nDevices:\n  Name: nested-device\n"
    assert plain_builder(valid)["status"] == "running"
    checks += 1
    for changed in (valid.replace("Driver: docker", "Driver: docker-container"),
                    valid + "\nName: other", valid + "\nNodes:\n",
                    valid.replace("Endpoint: default", "Endpoint: remote"),
                    valid.replace("Status: running", "Status: stopped"),
                    valid.replace("Name: default\nEndpoint:", "  Name: default\nEndpoint:"),
                    valid.replace("Name: default\nDriver:", "Name: default\nName: default\nDriver:"),
                    "  " + valid):
        reject(lambda text=changed: plain_builder(text))
    for name in ("../escape", "/absolute", "cmd\\escape", "cmd/a/../../x", "cmd/key.pem", "cmd/.env"):
        if secret_path(name):
            reject(lambda: require(not secret_path(name), "SECRET"))
        else:
            reject(lambda n=name: safe_archive_name(n))
    assert not secret_path("apps/admin-web/.env.example")
    checks += 1
    link = tarfile.TarInfo("cmd/link")
    link.type = tarfile.SYMTYPE
    reject(lambda: validate_member(link, "backend"))
    reject(lambda: validate_member(tarfile.TarInfo("outside"), "backend"))
    manifest, config = "sha256:" + "a" * 64, "sha256:" + "b" * 64
    meta = {"containerimage.digest": manifest, "containerimage.descriptor": {"digest": manifest},
            "containerimage.config.digest": config}
    assert metadata_identity(meta, config, {"id": config})["configDigest"] == config
    checks += 1
    reject(lambda: metadata_identity(meta, "sha256:" + "c" * 64, {"id": config}))
    reject(lambda: metadata_identity(meta, manifest, {"id": "sha256:" + "d" * 64}))
    reject(lambda: metadata_identity({"containerimage.digest": manifest, "containerimage.descriptor": {"digest": config}},
                                     manifest, {"id": manifest}))
    exported = {"containerimage.digest": manifest, "containerimage.config.digest": manifest}
    assert metadata_identity(exported, manifest, {"id": manifest}) == {
        "manifestDigest": manifest, "descriptorDigest": None, "configDigest": manifest,
        "iid": manifest, "identityScope": "ENGINE_ID_MATCHES_METADATA_CONFIG_DIGEST_NO_DESCRIPTOR"}
    checks += 1
    for metadata, exported_iid, exported_id in (
            ({"containerimage.digest": manifest}, manifest, manifest),
            ({**exported, "containerimage.config.digest": config}, manifest, manifest),
            (exported, config, manifest), (exported, manifest, config),
            ({**exported, "containerimage.descriptor": None}, manifest, manifest),
            ({**exported, "containerimage.descriptor": {"digest": config}}, manifest, manifest)):
        reject(lambda m=metadata, i=exported_iid, d=exported_id: metadata_identity(m, i, {"id": d}))
    safe = '{"ok":true,"code":"HEALTH_RESPONSE","statusCode":200,"status":"ok","service":"admin-web","rawSafeBody":"{\\"status\\":\\"ok\\",\\"service\\":\\"admin-web\\"}"}'
    assert safe_probe(safe)["ok"]
    checks += 1
    reject(lambda: safe_probe('{"ok":true,"code":"HTTP_CONNECT"}'))
    reject(lambda: safe_probe(safe.replace('"statusCode":200', '"statusCode":401')))
    reject(lambda: safe_probe('{"ok":false,"code":"PRIVATE_TOKEN"}'))
    reject(lambda: safe_probe(safe.replace('admin-web', 'private-value')))
    import copy
    node_env = ["PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
                "NODE_VERSION=22.19.0", "YARN_VERSION=1.22.22", "NODE_ENV=production",
                "HOSTNAME=0.0.0.0", "PORT=3000"]
    raw = {"Id": "c" * 64, "Image": manifest,
           "Config": {"Labels": {"openwa.packaging.owner": "owner"}, "Env": expected_env(node_env)},
           "State": {"Running": False, "OOMKilled": False, "ExitCode": 0},
           "HostConfig": {"NetworkMode": "none", "ReadonlyRootfs": True,
                          "Memory": 268435456, "MemorySwap": 268435456, "NanoCpus": 250000000,
                          "PidsLimit": 128, "RestartPolicy": {"Name": "no"}, "PortBindings": None,
                          "Binds": None, "Mounts": None, "Tmpfs": {"/tmp": "rw,size=64m"},
                          "CapDrop": ["ALL"], "SecurityOpt": ["no-new-privileges"]}, "Mounts": []}
    assert isolation_proof(raw, manifest, "owner", node_env)["envMatchesImagePlusExactOverrides"]
    checks += 1
    for key, value in (("NetworkMode", "bridge"), ("ReadonlyRootfs", False),
                       ("Memory", 0), ("MemorySwap", -1), ("NanoCpus", 0), ("PidsLimit", 0),
                       ("RestartPolicy", {"Name": "always"}), ("PortBindings", {"8080/tcp": [{}]}),
                       ("Binds", ["/host:/data"]), ("Tmpfs", {"/tmp": "rw,size=128m"}),
                       ("CapDrop", []), ("SecurityOpt", [])):
        changed = copy.deepcopy(raw)
        changed["HostConfig"][key] = value
        reject(lambda v=changed: isolation_proof(v, manifest, "owner", node_env))
    changed = copy.deepcopy(raw)
    changed["Mounts"] = [{"Type": "volume", "Destination": "/data"}]
    reject(lambda: isolation_proof(changed, manifest, "owner", node_env))
    changed = copy.deepcopy(raw)
    changed["Config"]["Env"].append("CONTROL_API_TOKEN=private")
    reject(lambda: isolation_proof(changed, manifest, "owner", node_env))
    reject(lambda: public_image_env(node_env + ["PRIVATE_URL=https://private"]))
    reject(lambda: isolation_proof(raw, manifest, "other-owner", node_env))
    reject(lambda: isolation_proof(raw, config, "owner", node_env))
    directory = tarfile.TarInfo("infrastructure")
    directory.type = tarfile.DIRTYPE
    assert validate_member(directory, "admin") == "infrastructure"
    checks += 1
    reject(lambda: validate_member(tarfile.TarInfo("infrastructure/unapproved"), "admin"))
    image = {"id": manifest, "os": "linux", "architecture": "amd64", "revision": ACCEPTED_SHA,
             "source": SOURCE_URL, "user": "campaign", "cmd": ["node", "server.js"], "entrypoint": ["/base-entrypoint"]}
    validate_image(image, "admin-web")
    checks += 1
    reject(lambda: validate_image({**image, "revision": "f" * 40}, "admin-web"))
    reject(lambda: validate_image({**image, "cmd": ["node", "other.js"]}, "admin-web"))
    reject(lambda: validate_image({**image, "architecture": "arm64"}, "admin-web"))
    print(json.dumps({"state": "PURE_VALIDATOR_SELF_TEST_PASS", "checks": checks,
                      "networkCalls": 0, "dockerCalls": 0, "productSuitesRun": 0}))
    return 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source")
    parser.add_argument("--tooling")
    parser.add_argument("--output-base")
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args()
    if args.self_test:
        return self_test()
    require(args.source and args.tooling and args.output_base, "CHECKOUT_AND_OUTPUT_ARGS_REQUIRED")
    return Packaging(args.source, args.tooling, args.output_base).execute()


if __name__ == "__main__":
    try:
        sys.exit(main())
    except GateFailure as error:
        # No arbitrary exception messages, environment, headers, or command output.
        print(json.dumps({"state": "FAILED_BEFORE_RECEIPT", "failureCode": str(error)}))
        sys.exit(1)
