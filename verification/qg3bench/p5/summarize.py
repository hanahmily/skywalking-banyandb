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
    for folder in sorted(out.iterdir()):
        if not folder.is_dir() or not (folder/'run.json').exists():continue
        record=json.loads((folder/'run.json').read_text());assert record['exit_code']==0
        assert record['log_sha256']==hashlib.sha256((folder/'run.log').read_bytes()).hexdigest()
        assert record['observations_sha256']==hashlib.sha256((folder/'observations.jsonl').read_bytes()).hexdigest()
        assert '\nPASS\n' in (folder/'run.log').read_text()
        rows=[json.loads(line) for line in (folder/'observations.jsonl').read_text().splitlines()]
        for row in rows:
            row['run']=folder.name
            if row['phase'] in ('timing','cache'):
                ordered=sorted(row['samples_ns']);assert len(ordered)==100 and row['successes']==100
                assert row['p50_ns']==ordered[49] and row['p95_ns']==ordered[94]
                assert row['native_after']['Executions']==row['native_before']['Executions']
                before,after=row['cache_before'],row['cache_after']
                if row['bypass']:
                    assert after['Bypass']-before['Bypass']==100 and after['Compiles']-before['Compiles']==100
                else:
                    assert after['Hits']-before['Hits']==100 and after['Compiles']==before['Compiles']
                for category in ('group_expression','projection','native','aggregate'):
                    assert row[category+'_after']['LiveBytes']==0
                    row[category+'_delta']={key:row[category+'_after'][key]-row[category+'_before'][key] for key in row[category+'_before']}
            if row['phase'] in ('timing','cache'):
                state_per_group=32 if 'decimal' in row['arm'] else 16
                groups=8 if row['arm'].startswith('group') else 1
                assert row['aggregate_delta']['StateBytes']==state_per_group*groups*100
            if row['phase']=='memory':
                assert row['requests']==1 and row['owner_pins']==0 and row['memory_after']['LiveReservedBytes']==0
                assert all(row['scan'][key]==0 for key in ('CursorRefs','OwnerPins','QueryResults','ReservedBytes','DecodeTasks'))
                assert row['batch']['LiveResults']==0
                assert all(row['cache'][key]==0 for key in ('InFlight','CompileReservedBytes','UncachedBytes'))
                assert all(row[key]['LiveBytes']==0 for key in ('group_expression','projection','native','aggregate_after'))
        observations.extend(rows)
        record['cgroup_peak_bytes']=int((folder/'memory-peak.txt').read_text())
        def cpu(path):return {k:int(v) for k,v in (line.split() for line in path.read_text().splitlines())}
        before,after=cpu(folder/'cpu-before.txt'),cpu(folder/'cpu-after.txt')
        record['cgroup_cpu_delta']={key:after[key]-before[key] for key in before}
        runs.append(record)
    decision=json.loads((out/'profile-decision.json').read_text())
    assert not decision['high_group_needed']
    assert len(runs)==(37 if decision['profile_triggered'] else 34),len(runs)
    measured=[r for r in observations if r['phase'] in ('timing','cache')]
    assert len(measured)==70 and sum(r['successes'] for r in measured)==7000
    assert len([r for r in observations if r['phase']=='qualify' and r['success']])==8
    assert len([r for r in observations if r['phase']=='memory'])==8
    assert len([r for r in observations if r['phase']=='profile'])==(4 if decision['profile_triggered'] else 0)
    def cohort(size,arm,revision='current',mode='timing',bypass=False):
        suffix=revision+'-'+('bypass' if bypass else 'cache')
        group=sorted([r for r in measured if r['run'].startswith(f'{mode}-{size}-') and r['run'].endswith(suffix) and r['arm']==arm],key=lambda r:r['run'])
        assert len(group)==5,(size,arm,revision,mode,bypass,len(group))
        return group
    comparisons={}
    for size in (64,8192):
        for arm in ('scalar_int','group_int'):
            comparisons[f'{size}_{arm}_base_to_current']=paired(cohort(size,arm,'base'),cohort(size,arm))
            for index in range(5):
                requests=[r for r in observations if r['phase']=='request' and r['arm']==arm and r['run'] in (f'timing-{size}-{index}-base-cache',f'timing-{size}-{index}-current-cache')]
                assert len(requests)==2 and requests[0]['request_sha256']==requests[1]['request_sha256'] and requests[0]['wants']==requests[1]['wants']
        for direct,feature in (('scalar_int','scalar_decimal'),('group_int','group_decimal')):
            comparisons[f'{size}_{feature}_over_direct']=paired(cohort(size,direct),cohort(size,feature))
    comparisons['64_decimal_cache_to_bypass']=paired(cohort(64,'scalar_decimal',mode='cache'),cohort(64,'scalar_decimal',mode='cache',bypass=True))
    write(out/'observations.json',observations);write(out/'runs.json',runs);write(out/'comparisons.json',comparisons)
    lines=['| Rows | Revision | Arm | Mean process p50 ms [range] | Mean process p95 ms | CPU ms/RPC | Allocated B/RPC | Objects/RPC |',
           '|---:|---|---|---:|---:|---:|---:|---:|']
    for size in (64,8192):
        for revision in ('base','current'):
            for arm in sorted({r['arm'] for r in measured if r['phase']=='timing' and r['run'].endswith(revision+'-cache')}):
                group=cohort(size,arm,revision);mean=lambda key:statistics.mean(r[key] for r in group)
                p50=[r['p50_ns']/1e6 for r in group]
                lines.append(f'| {size} | {revision} | {arm} | {statistics.mean(p50):.3f} [{min(p50):.3f}, {max(p50):.3f}] | {mean("p95_ns")/1e6:.3f} | {mean("cpu_ns_per_rpc")/1e6:.3f} | {mean("allocated_bytes_per_rpc"):.0f} | {mean("objects_per_rpc"):.0f} |')
    (out/'table.md').write_text('\n'.join(lines)+'\n')


if __name__=='__main__':
    parser=argparse.ArgumentParser();parser.add_argument('out',type=Path);main(parser.parse_args().out)
