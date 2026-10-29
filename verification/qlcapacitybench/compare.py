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
"""Compare frozen P1 binaries on the four previously admitted arms only."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import statistics

import run as campaign
from summarize import bootstrap, percentile

ARMS = ("in_10", "in_1000", "in_10000", "in_10_bypass")


def prepare(source, destination):
    manifest = json.loads((source / "manifest.json").read_text())
    binary = source / "bench.test"
    if campaign.sha(binary) != manifest["binaries"]["bench"]:
        raise SystemExit("frozen benchmark binary mismatch: " + str(source))
    destination.mkdir(parents=True, exist_ok=True)
    if any(destination.glob("round-*.jsonl")):
        raise SystemExit("refuse to overwrite existing comparison: " + str(destination))
    shutil.copy2(binary, destination / "bench.test")
    shutil.copy2(source / "manifest.json", destination / "manifest.json")
    return manifest


def observations(directory):
    result = {}
    for round_number in range(5):
        path = directory / f"round-{round_number}.jsonl"
        rows = [json.loads(line) for line in path.read_text().splitlines()]
        measured = [row for row in rows if row["phase"] == "round"]
        if {row["arm"] for row in measured} != set(ARMS):
            raise SystemExit("unexpected comparison arms: " + str(path))
        for row in measured:
            if row["successes"] != 200 or len(row["samples_ns"]) != 200 or row.get("error"):
                raise SystemExit("incomplete successful cohort: " + str(path))
            row["p50_ms"] = percentile(row["samples_ns"], .5) / 1e6
            row["p95_ms"] = percentile(row["samples_ns"], .95) / 1e6
            result[(round_number, row["arm"])] = row
    return result


def summarize(directory):
    before = observations(directory / "baseline")
    after = observations(directory / "candidate")
    result = {"arms": {}, "interpretation": "Five paired fresh processes per binary, alternating binary order; same query and cohort. Intervals resample process pairs. No post-hoc percentage gate."}
    for arm in ARMS:
        pairs = [(before[(n, arm)], after[(n, arm)]) for n in range(5)]
        for left, right in pairs:
            for key in ("query_digest", "result_digests", "request_bytes", "list_bytes"):
                if left[key] != right[key]:
                    raise SystemExit("unequal comparison work: " + arm + " " + key)
            if left["batch"]["NativeInputRows"] != 16000 or right["batch"]["NativeInputRows"] != 16000:
                raise SystemExit("source rows changed: " + arm)
        result["arms"][arm] = {}
        for metric in ("p50_ms", "p95_ms", "cpu_ns_per_rpc", "allocated_bytes_per_rpc", "allocated_objects_per_rpc"):
            values = [(left[metric], right[metric]) for left, right in pairs]
            result["arms"][arm][metric] = {
                "baseline_mean": statistics.mean(left for left, _ in values),
                "candidate_mean": statistics.mean(right for _, right in values),
                "pairs": values, **bootstrap(values),
            }
        entry = result["arms"][arm]
        print(f"{arm}: p50 {entry['p50_ms']['baseline_mean']:.3f} -> {entry['p50_ms']['candidate_mean']:.3f} ms; "
              f"allocation {entry['allocated_bytes_per_rpc']['baseline_mean']:.0f} -> {entry['allocated_bytes_per_rpc']['candidate_mean']:.0f} B")
    (directory / "comparison.json").write_text(json.dumps(result, indent=2) + "\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("run", "summarize"))
    parser.add_argument("--baseline", type=Path)
    parser.add_argument("--candidate", type=Path)
    parser.add_argument("--output", type=Path, required=True)
    options = parser.parse_args()
    directory = options.output.resolve()
    if options.action == "summarize":
        summarize(directory)
        return
    if options.baseline is None or options.candidate is None:
        parser.error("run requires --baseline and --candidate frozen build directories")
    campaign.verify_frozen(options.candidate.resolve())
    manifests = {name: prepare(source.resolve(), directory / name) for name, source in
                 (("baseline", options.baseline), ("candidate", options.candidate))}
    for key in ("go", "image", "fixture", "protocol"):
        if manifests["baseline"][key] != manifests["candidate"][key]:
            raise SystemExit("incompatible benchmark settings: " + key)
    schedule = [(n, list(("baseline", "candidate") if n % 2 == 0 else ("candidate", "baseline"))) for n in range(5)]
    (directory / "comparison-manifest.json").write_text(json.dumps({
        "runner_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        "arms": ARMS, "schedule": schedule, "manifests": manifests,
    }, indent=2) + "\n")
    for round_number, order in schedule:
        for name in order:
            code = campaign.run(directory / name, "bench", "round", round_number, ",".join(ARMS))
            if code:
                raise SystemExit(code)
    campaign.verify_frozen(options.candidate.resolve())
    summarize(directory)


if __name__ == "__main__":
    main()
