#!/usr/bin/env python3
# Licensed to Apache Software Foundation (ASF) under one or more contributor
# license agreements. See the NOTICE file distributed with
# this work for additional information regarding copyright
# ownership. Apache Software Foundation (ASF) licenses this file to you under
# the Apache License, Version 2.0 (the "License"); you may
# not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing,
# software distributed under the License is distributed on an
# "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
# KIND, either express or implied. See the License for the
# specific language governing permissions and limitations
# under the License.
"""Pinned P1 correctness and fixed five-process cost/capacity campaign."""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[2]
HERE = Path(__file__).resolve().parent
PIN = "efc0c18988ecbc18a10ffbdd49c7491299d8c8e7"
IMAGE = "banyandb-distributed-querybench:go1.26.10"


def command(args, **kwargs):
    return subprocess.check_output(args, cwd=ROOT, text=True, **kwargs).strip()


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def source_hash():
    paths = command(["git", "ls-files", "-co", "--exclude-standard"]).splitlines()
    digest = hashlib.sha256()
    for name in sorted(set(paths)):
        path = ROOT / name
        if path.is_file() and (name.endswith(".go") or name in ("go.mod", "go.sum") or name == "test/cases/measure/qlgen/generated/suite.json"):
            digest.update(name.encode() + b"\0" + bytes.fromhex(sha(path)))
    digest.update(Path(__file__).read_bytes())
    return digest.hexdigest()


def identity(out):
    return {
        "utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "revision": command(["git", "rev-parse", "HEAD"]),
        "branch": command(["git", "branch", "--show-current"]),
        "source_sha256": source_hash(),
        "runner_sha256": sha(__file__),
        "go": command(["go", "version"]),
        "image": json.loads(command(["docker", "image", "inspect", IMAGE]))[0]["Id"],
        "build_tags": ["querymetrics"],
        "seed": 1412,
        "fixture": {"services": 1024, "entities_per_service": 5, "active_per_service": 4, "timestamps": 2, "rows": 10240, "active_distinct": 4096, "positive_rows": 8192},
        "protocol": {"rounds": 5, "warmups": 20, "timed": 200, "callers": 1, "query_allowance_mib": 256, "process_protector": "default cgroup-derived"},
        "binaries": {name: sha(out / (name + ".test")) for name in ("standalone", "distributed", "bench")},
    }


def build(out):
    if command(["git", "rev-parse", "HEAD"]) != PIN:
        raise SystemExit("refuse unintended revision")
    out.mkdir(parents=True, exist_ok=True)
    for name, package in (("standalone", "./test/integration/standalone/query"), ("distributed", "./test/integration/distributed/query"), ("bench", "./verification/qlcapacitybench")):
        subprocess.run(["go", "test", "-c", "-tags=querymetrics", "-o", str(out / (name + ".test")), package], cwd=ROOT, check=True)
    (out / "manifest.json").write_text(json.dumps(identity(out), indent=2) + "\n")


def verify_frozen(out):
    manifest = json.loads((out / "manifest.json").read_text())
    actual = identity(out)
    for key in ("revision", "source_sha256", "go", "image", "binaries"):
        if manifest[key] != actual[key]:
            raise SystemExit("frozen identity changed: " + key + "; build a new campaign")


def run(out, name, mode=None, round_number=0, admitted="", probe=""):
    topology = name if mode is None else "standalone"
    distributed = topology == "distributed"
    cpu, memory = (4, 8) if distributed else (2, 4)
    label = topology if mode is None else f"{mode}-{round_number}"
    container_name = "ql-p1-" + label
    args = ["docker", "run", "--rm", "--name", container_name, f"--cpus={cpu}", f"--memory={memory}g", f"--memory-swap={memory}g", "-e", f"GOMAXPROCS={cpu}", "-e", "P1_EXPECT_WORKERS=" + ("2" if distributed else "1"), "-e", "P1_VERIFY=1", "-v", str(ROOT) + ":" + str(ROOT) + ":ro", "-v", str(out) + ":/evidence", "-w", str(ROOT / ("test/integration/" + topology + "/query" if mode is None else "verification/qlcapacitybench"))]
    if mode:
        args += ["-e", "P1_MODE=" + mode, "-e", "P1_OUTPUT=/evidence/" + label + ".jsonl", "-e", "P1_ROUND=" + str(round_number), "-e", "P1_ADMITTED=" + admitted]
    if probe:
        args += ["-e", "P1_PROBE_ARM=" + probe]
    args += [IMAGE, "/evidence/" + name + ".test", "-test.timeout=45m"]
    if mode is None:
        args += ["-ginkgo.label-filter=measure-ql && QL-P1", "-ginkgo.fail-on-empty", "-ginkgo.no-color", "-ginkgo.json-report=/evidence/" + label + ".ginkgo.json"]
    else:
        args += ["-test.run=^TestP1RPC$", "-test.v"]
    log = out / (label + ".log")
    record = {"utc": datetime.datetime.now(datetime.timezone.utc).isoformat(), "command": args, "load_before": Path("/proc/loadavg").read_text().strip()}
    start = time.monotonic()
    with log.open("w") as stream:
        process = subprocess.Popen(args, stdout=stream, stderr=subprocess.STDOUT)
        # Capture Docker's observed resource configuration while this exact container runs.
        for _ in range(50):
            if process.poll() is not None:
                break
            inspected = subprocess.run(["docker", "inspect", container_name], text=True, capture_output=True)
            if inspected.returncode == 0:
                info = json.loads(inspected.stdout)[0]
                record["resources"] = {key: info["HostConfig"][key] for key in ("NanoCpus", "Memory", "MemorySwap", "CpusetCpus")}
                record["image_id"] = info["Image"]
                break
            time.sleep(.1)
        exit_code = process.wait()
    record.update(exit_code=exit_code, wall_seconds=time.monotonic() - start, load_after=Path("/proc/loadavg").read_text().strip())
    if mode is None and (out / (label + ".ginkgo.json")).exists():
        report = json.loads((out / (label + ".ginkgo.json")).read_text())[0]
        specs = [spec for spec in report["SpecReports"] if spec["State"] not in ("skipped", "pending") and spec["LeafNodeType"] == "It"]
        record["specs"] = [{"name": spec["LeafNodeText"], "state": spec["State"], "failure": spec.get("Failure", {}).get("Message", ""), "evidence": [line for line in spec.get("CapturedGinkgoWriterOutput", "").splitlines() if "P1 " in line]} for spec in specs]
    (out / (label + "-run.json")).write_text(json.dumps(record, indent=2) + "\n")
    print(json.dumps({"run": label, "exit": exit_code, "wall_seconds": record["wall_seconds"]}), flush=True)
    if exit_code:
        print("\n".join(log.read_text().splitlines()[-35:]), flush=True)
    return exit_code


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("build", "correctness", "feasibility", "measure", "memory"))
    parser.add_argument("--output", type=Path, default=Path("/tmp/ql-p1-campaign"))
    parser.add_argument("--probe-arm", default="", help="memory-mode cancellation/concurrency arm; defaults to in_10000")
    options = parser.parse_args()
    out = options.output.resolve()
    if options.action == "build":
        build(out)
        return
    verify_frozen(out)
    if options.action == "correctness":
        codes = [run(out, topology) for topology in ("standalone", "distributed")]
        raise SystemExit(max(codes))
    if options.action == "feasibility":
        raise SystemExit(run(out, "bench", options.action))
    observations = [json.loads(line) for line in (out / "feasibility-0.jsonl").read_text().splitlines()]
    admitted = [value["arm"] for value in observations if value["phase"] == "feasibility" and value.get("successes") == 2 and not value.get("rejections")]
    if not admitted:
        raise SystemExit("no admitted arms")
    if options.action == "memory":
        raise SystemExit(run(out,"bench","memory",admitted=",".join(admitted),probe=options.probe_arm))
    # A wrong answer or unexpected failure never becomes a capacity result or a timing arm.
    feasibility_run = json.loads((out / "feasibility-0-run.json").read_text())
    if feasibility_run["exit_code"]:
        raise SystemExit("feasibility did not complete")
    for round_number in range(5):
        code = run(out, "bench", "round", round_number, ",".join(admitted))
        if code:
            raise SystemExit(code)


if __name__ == "__main__":
    main()
