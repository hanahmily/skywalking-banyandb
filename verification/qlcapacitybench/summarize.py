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
"""Summarize fixed P1 process rounds without treating RPCs as independent replicates."""
import argparse
import hashlib
import json
from pathlib import Path
import random
import statistics


def percentile(values, quantile):
    ordered = sorted(values)
    return ordered[min(len(ordered) - 1, int((len(ordered) - 1) * quantile))]


def bootstrap(pairs):
    rng = random.Random(1412)
    effects, ratios = [], []
    for _ in range(10000):
        selected = rng.choices(pairs, k=len(pairs))
        effects.append(statistics.mean(right - left for left, right in selected))
        ratios.append(statistics.mean(right / left for left, right in selected))
    return {"mean_effect": statistics.mean(right - left for left, right in pairs), "effect_95pct": [percentile(effects, .025), percentile(effects, .975)], "mean_ratio": statistics.mean(right / left for left, right in pairs), "ratio_95pct": [percentile(ratios, .025), percentile(ratios, .975)]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("campaign", type=Path)
    options = parser.parse_args()
    by_arm = {}
    for path in sorted(options.campaign.glob("round-*.jsonl")):
        for line in path.read_text().splitlines():
            value = json.loads(line)
            if value["phase"] != "round":
                continue
            if value["successes"] != 200 or len(value["samples_ns"]) != 200:
                raise SystemExit("incomplete fixed cohort: " + str(path))
            value["p50_ms"] = percentile(value["samples_ns"], .5) / 1e6
            value["p95_ms"] = percentile(value["samples_ns"], .95) / 1e6
            by_arm.setdefault(value["arm"], []).append(value)
    result = {"analysis_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(), "processes": {}, "paired": {}, "interpretation": "Five fresh-process diagnostic rounds; no regression/SLO percentage threshold. Bootstrap resamples paired processes, never individual RPCs."}
    for name, rounds in by_arm.items():
        if len(rounds) != 5 or sorted(value["round"] for value in rounds) != list(range(5)):
            raise SystemExit("missing fixed process round: " + name)
        result["processes"][name] = [{key: value[key] for key in ("round", "request_bytes", "list_bytes", "successes", "p50_ms", "p95_ms", "cpu_ns_per_rpc", "allocated_bytes_per_rpc", "allocated_objects_per_rpc", "gc_cpu_seconds_per_rpc", "batch")} for value in rounds]
        print(f"{name}: p50={statistics.mean(value['p50_ms'] for value in rounds):.3f}ms p95={statistics.mean(value['p95_ms'] for value in rounds):.3f}ms CPU={statistics.mean(value['cpu_ns_per_rpc'] for value in rounds)/1e6:.3f}ms alloc={statistics.mean(value['allocated_bytes_per_rpc'] for value in rounds):.0f}B")
    control = {value["round"]: value for value in by_arm.get("in_10", [])}
    for name in ("in_1000", "in_10000", "in_10_bypass"):
        if name not in by_arm or not control:
            continue
        rounds = by_arm[name]
        result["paired"][name + "/in_10"] = {metric: bootstrap([(control[value["round"]][metric], value[metric]) for value in rounds]) for metric in ("p50_ms", "p95_ms", "allocated_bytes_per_rpc", "cpu_ns_per_rpc")}
    (options.campaign / "summary.json").write_text(json.dumps(result, indent=2) + "\n")


if __name__ == "__main__":
    main()
