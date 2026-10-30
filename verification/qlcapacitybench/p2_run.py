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
"""Pinned P2 shard/stage correctness and bounded distinct-capacity campaign."""
import argparse
import datetime
import hashlib
import json
from pathlib import Path
import subprocess
import time

ROOT = Path(__file__).resolve().parents[2]
HERE = Path(__file__).resolve().parent
PIN = "5e78df39d0ca171aa61b97dbb4383dab32c434e6"
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
        if path.is_file() and (name.endswith(".go") or name in ("go.mod", "go.sum") or name == "test/cases/measure/qlgen/generated/suite.json" or (name.startswith("verification/qlcapacitybench/") and name.endswith(".py"))):
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
        "fixture": {"arms": {
            "d1000": {"rows": 50000, "distinct_entries": 1000, "groups": 8, "key_width": 16},
            "d10000": {"rows": 50000, "distinct_entries": 10000, "groups": 8, "key_width": 16},
            "d50000": {"rows": 50000, "distinct_entries": 50000, "groups": 8, "key_width": 16},
            "r10000": {"rows": 10000, "distinct_entries": 1000, "groups": 8, "key_width": 16},
            "r100000": {"rows": 100000, "distinct_entries": 1000, "groups": 8, "key_width": 16},
            "g1": {"rows": 50000, "distinct_entries": 1000, "groups": 1, "key_width": 16},
            "g64": {"rows": 50000, "distinct_entries": 1000, "groups": 64, "key_width": 16},
            "w256": {"rows": 50000, "distinct_entries": 1000, "groups": 8, "key_width": 256},
        }},
        "protocol": {"rounds": 5, "warmups": 3, "timed": 30, "callers": 1, "query_allowance_mib": 256, "process_protector": "default cgroup-derived"},
        "binaries": {name: sha(out / (name + ".test")) for name in ("standalone", "distributed", "bench")},
    }


def build(out):
    if command(["git", "rev-parse", "HEAD"]) != PIN:
        raise SystemExit("refuse unintended revision")
    out.mkdir(parents=True, exist_ok=True)
    if (out / "manifest.json").exists() or any(out.glob("*-run.json")):
        raise SystemExit("refuse to overwrite a frozen campaign; choose a new output directory")
    before = source_hash()
    for name, package in (("standalone", "./test/integration/standalone/query"), ("distributed", "./test/integration/distributed/query"), ("bench", "./verification/qlcapacitybench")):
        subprocess.run(["go", "test", "-c", "-tags=querymetrics", "-o", str(out / (name + ".test")), package], cwd=ROOT, check=True)
    manifest = identity(out)
    if manifest["source_sha256"] != before:
        raise SystemExit("source changed during build; build again before freezing evidence")
    (out / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")


def verify_frozen(out):
    manifest = json.loads((out / "manifest.json").read_text())
    actual = identity(out)
    for key in ("revision", "source_sha256", "go", "image", "binaries"):
        if manifest[key] != actual[key]:
            raise SystemExit("frozen identity changed: " + key + "; build a new campaign")


def run(out, name, mode=None, round_number=0, admitted=""):
    topology = name if mode is None else "standalone"
    distributed = topology == "distributed"
    stages = mode == "stages"
    cpu, memory = (4, 8) if distributed or stages else (2, 4)
    label = topology if mode is None else ("stages" if stages else f"{mode}-{round_number}")
    if (out / (label + "-run.json")).exists() or (out / (label + ".log")).exists():
        raise SystemExit("refuse to overwrite run " + label)
    container_name = "ql-p2-" + label
    args = ["docker", "run", "--rm", "--name", container_name, f"--cpus={cpu}", f"--memory={memory}g", f"--memory-swap={memory}g", "-e", f"GOMAXPROCS={cpu}", "-e", "P1_EXPECT_WORKERS=" + ("2" if distributed else "1"), "-e", "P2_VERIFY=1", "-v", str(ROOT) + ":" + str(ROOT) + ":ro", "-v", str(out) + ":/evidence", "-w", str(ROOT / ("test/integration/" + topology + "/query" if mode is None else "verification/qlcapacitybench"))]
    if mode and not stages:
        args += ["-e", "P2_MODE=" + mode, "-e", "P2_OUTPUT=/evidence/" + label + ".jsonl", "-e", "P2_ROUND=" + str(round_number), "-e", "P2_ADMITTED=" + admitted]
    if stages:
        args += ["-e", "P2_STAGE_VERIFY=1", "-e", "P2_STAGE_OUTPUT=/evidence/stages.json"]
    args += [IMAGE, "/evidence/" + name + ".test", "-test.timeout=45m"]
    if mode is None:
        args += ["-ginkgo.label-filter=measure-ql && QL-P2", "-ginkgo.fail-on-empty", "-ginkgo.no-color", "-ginkgo.json-report=/evidence/" + label + ".ginkgo.json"]
    else:
        args += ["-test.run=^TestP2Stages$" if stages else "-test.run=^TestP2RPC$", "-test.v"]
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
        record["specs"] = [{"name": spec["LeafNodeText"], "state": spec["State"], "failure": spec.get("Failure", {}).get("Message", ""), "evidence": [line for line in spec.get("CapturedGinkgoWriterOutput", "").splitlines() if "P2 " in line]} for spec in specs]
    (out / (label + "-run.json")).write_text(json.dumps(record, indent=2) + "\n")
    print(json.dumps({"run": label, "exit": exit_code, "wall_seconds": record["wall_seconds"]}), flush=True)
    if exit_code:
        print("\n".join(log.read_text().splitlines()[-35:]), flush=True)
    return exit_code


def require_success(out, label):
    path = out / (label + "-run.json")
    if not path.exists() or json.loads(path.read_text())["exit_code"] != 0:
        raise SystemExit("missing successful prerequisite: " + label)


def require_standalone_admission_gap(out):
    report = json.loads((out / "standalone-run.json").read_text())
    specs = report.get("specs", [])
    rejections = [line for spec in specs for line in spec.get("evidence", []) if line.startswith("P2 capacity rejection:")]
    summaries = [line for spec in specs for line in spec.get("evidence", []) if line.startswith("P2 generated:")]
    if (report["exit_code"] != 1 or len(specs) != 3 or any(spec["state"] != "failed" for spec in specs)
            or any("P2 full-cohort correctness remains unqualified when native admission rejects" not in spec["failure"] for spec in specs)
            or len(rejections) != 8 or any("code = ResourceExhausted" not in line for line in rejections)
            or not summaries or not any("verification_rpcs=48" in line for line in summaries)):
        raise SystemExit("standalone failure is not the strictly qualified P2 native-admission gap")
    evidence = {"standalone_status": "failed_capacity_qualification", "attempts": 48,
                "correct": 40, "resource_exhausted": 8,
                "scope": "independent count-only capacity timings; standalone full SUM remains unqualified"}
    (out / "standalone-admission-exception.json").write_text(json.dumps(evidence, indent=2) + "\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("build", "correctness", "stages", "feasibility", "measure", "memory"))
    parser.add_argument("--allow-standalone-admission-gap", action="store_true", help="explicitly retain the strict 40-correct/8-native-rejection qualification while timing independent admitted count-only arms")
    parser.add_argument("--output", type=Path, default=Path("/tmp/ql-p2-campaign"))
    options = parser.parse_args()
    out = options.output.resolve()
    if options.action == "build":
        build(out)
        return
    verify_frozen(out)
    if options.action == "correctness":
        codes = [run(out, topology) for topology in ("standalone", "distributed")]
        raise SystemExit(max(codes))
    if options.action == "stages":
        raise SystemExit(run(out, "bench", "stages"))
    if options.action == "feasibility":
        raise SystemExit(run(out, "bench", "feasibility"))
    require_success(out, "feasibility-0")
    observations = [json.loads(line) for line in (out / "feasibility-0.jsonl").read_text().splitlines()]
    admitted = [value["arm"] for value in observations if value["phase"] == "feasibility" and value.get("successes") == 1 and not value.get("rejections")]
    if options.action == "memory":
        raise SystemExit(run(out, "bench", "memory", admitted=",".join(admitted)))
    if not admitted:
        raise SystemExit("no admitted arms to time; retain the capacity boundary report")
    require_success(out, "distributed")
    if options.allow_standalone_admission_gap:
        require_standalone_admission_gap(out)
    else:
        require_success(out, "standalone")
    # A stage blocker remains an independent checkpoint; never relabel it as passed.
    for round_number in range(5):
        code = run(out, "bench", "round", round_number, ",".join(admitted))
        if code:
            raise SystemExit(code)
    verify_frozen(out)


if __name__ == "__main__":
    main()
