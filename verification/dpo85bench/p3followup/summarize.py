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
"""Validate raw campaign and summarize process-level paired observations."""
import argparse
import hashlib
import json
import math
from pathlib import Path
import random
import statistics


def write(path,data):
    path.write_text(json.dumps(data,indent=2)+'\n')


def paired(left,right):
    differences=[b['p50_ns']-a['p50_ns'] for a,b in zip(left,right)]
    log_ratios=[math.log(b['p50_ns']/a['p50_ns']) for a,b in zip(left,right)]
    rng=random.Random(1412);delta_samples=[];ratio_samples=[]
    for _ in range(20000):
        indices=[rng.randrange(5) for _ in range(5)]
        delta_samples.append(statistics.mean(differences[i] for i in indices)/1e6)
        ratio_samples.append(math.exp(statistics.mean(log_ratios[i] for i in indices)))
    delta_samples.sort();ratio_samples.sort()
    return {'pairs':5,'bootstrap_draws':20000,'seed':1412,
            'mean_paired_p50_delta_ms':statistics.mean(differences)/1e6,
            'p50_geomean_ratio':math.exp(statistics.mean(log_ratios)),
            'delta_ms_percentile_90_interval':[delta_samples[999],delta_samples[18999]],
            'ratio_percentile_90_interval':[ratio_samples[999],ratio_samples[18999]],
            'paired_p50_delta_ms':[d/1e6 for d in differences],
            'paired_p50_ratios':[math.exp(r) for r in log_ratios],
            'allocated_bytes_mean_paired_delta':statistics.mean(b['allocated_bytes_per_rpc']-a['allocated_bytes_per_rpc'] for a,b in zip(left,right)),
            'allocated_bytes_geomean_ratio':math.exp(statistics.mean(math.log(b['allocated_bytes_per_rpc']/a['allocated_bytes_per_rpc']) for a,b in zip(left,right))),
            'paired_allocated_bytes_deltas':[b['allocated_bytes_per_rpc']-a['allocated_bytes_per_rpc'] for a,b in zip(left,right)],
            'cpu_ms_mean_paired_delta':statistics.mean(b['cpu_ns_per_rpc']-a['cpu_ns_per_rpc'] for a,b in zip(left,right))/1e6,
            'interpretation':'Fixed diagnostic; no practical tolerance declared, not a noninferiority/regression certification.'}


def main(out):
    observations=[];runs=[]
    expected=35
    for folder in sorted(out.iterdir()):
        if not folder.is_dir() or not (folder/'run.json').exists():continue
        record=json.loads((folder/'run.json').read_text());assert record['exit_code']==0
        assert record['log_sha256']==hashlib.sha256((folder/'run.log').read_bytes()).hexdigest()
        assert '\nPASS\n' in (folder/'run.log').read_text()
        manifest=json.loads((out/'manifest.json').read_text())
        digest=record.get('observations_sha256') or manifest.get('preserved_evidence',{}).get(folder.name)
        assert digest==hashlib.sha256((folder/'observations.jsonl').read_bytes()).hexdigest()
        rows=[json.loads(line) for line in (folder/'observations.jsonl').read_text().splitlines()]
        for row in rows:
            row['run']=folder.name
            if row['phase']=='request':
                row['canonical_request_sha256']=hashlib.sha256(json.dumps(row['request'],sort_keys=True,separators=(',',':')).encode()).hexdigest()
            if row['phase'] in ('timing','exact','cache'):
                samples=sorted(row['samples_ns']);assert len(samples)==100 and row['successes']==100
                assert row['p50_ns']==samples[49] and row['p95_ns']==samples[94]
                if row['phase']=='cache':
                    before,after=row['cache_before'],row['cache_after']
                    if row['bypass']:
                        assert after['Bypass']-before['Bypass']==100 and after['Compiles']-before['Compiles']==100
                    else:
                        assert after['Hits']-before['Hits']==100 and after['Compiles']==before['Compiles']
                assert row['native_rows']==(0 if 'no_match' in row['arm'] else 200 if row['arm']=='exact_control' else 1600)
            if row['phase']=='memory':
                assert row['requests']==1 and row['owner_pins']==0 and row['memory_after']['LiveReservedBytes']==0
                assert all(row['scan'][k]==0 for k in ('CursorRefs','OwnerPins','QueryResults','ReservedBytes','DecodeTasks'))
                assert row['batch']['LiveResults']==0
                assert all(row['cache'][k]==0 for k in ('InFlight','CompileReservedBytes','UncachedBytes'))
        observations.extend(rows)
        record['cgroup_peak_bytes']=int((folder/'memory-peak.txt').read_text())
        def cpu(path):return {k:int(v) for k,v in (line.split() for line in path.read_text().splitlines())}
        before,after=cpu(folder/'cpu-before.txt'),cpu(folder/'cpu-after.txt')
        record['cgroup_cpu_delta']={key:after[key]-before[key] for key in before}
        runs.append(record)
    assert len(runs)==expected, (len(runs),expected)
    measured=[r for r in observations if r['phase'] in ('timing','exact','cache')]
    assert len(measured)==90 and sum(r['successes'] for r in measured)==9000
    assert len([r for r in observations if r['phase']=='memory'])==14
    assert len([r for r in observations if r['phase']=='qualify' and r['success']])==14
    assert len([r for r in observations if r['phase']=='cache_assertion'])==10
    def cohort(prefix,suffix):
        return sorted([r for r in measured if r['run'].startswith(prefix) and r['run'].endswith(suffix)],key=lambda r:r['run'])
    for index in range(5):
        requests=[r for r in observations if r['phase']=='request' and r['run'] in (f'exact-10000-{index}-base-cache',f'exact-10000-{index}-current-cache')]
        assert len(requests)==2 and requests[0]['canonical_request_sha256']==requests[1]['canonical_request_sha256']
    comparisons={'base_to_current':paired(cohort('exact-','base-cache'),cohort('exact-','current-cache')),
                 'cached_to_bypass':paired(cohort('cache-','current-cache'),cohort('cache-','current-bypass'))}
    write(out/'observations.json',observations);write(out/'runs.json',runs);write(out/'comparisons.json',comparisons)
    lines=['| Names | Arm | Mean process p50 ms [range] | Mean process p95 ms | CPU ms/RPC | Allocated B/RPC | Objects/RPC |',
           '|---:|---|---:|---:|---:|---:|---:|']
    for size in (1000,10000):
        for arm in sorted({r['arm'] for r in measured if r['phase']=='timing'}):
            group=[r for r in measured if r['phase']=='timing' and r['run'].startswith(f'timing-{size}-') and r['arm']==arm]
            assert len(group)==5
            mean=lambda key:statistics.mean(r[key] for r in group)
            p50=[r['p50_ns']/1e6 for r in group]
            lines.append(f'| {size} | {arm} | {statistics.mean(p50):.3f} [{min(p50):.3f}, {max(p50):.3f}] | {mean("p95_ns")/1e6:.3f} | {mean("cpu_ns_per_rpc")/1e6:.3f} | {mean("allocated_bytes_per_rpc"):.0f} | {mean("objects_per_rpc"):.0f} |')
    (out/'table.md').write_text('\n'.join(lines)+'\n')


if __name__=='__main__':
    parser=argparse.ArgumentParser();parser.add_argument('out',type=Path);main(parser.parse_args().out)
