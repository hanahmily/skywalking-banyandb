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
"""Validate P6 full outputs and ownership without executing another query."""
import argparse
import hashlib
import json
from pathlib import Path


def sha(path):return hashlib.sha256(path.read_bytes()).hexdigest()
def idle(owner):
    assert owner['memory']['LiveReservedBytes']==0 and owner['owner_pins']==0
    assert owner['batch']['LiveResults']==0
    assert all(owner['scan'][key]==0 for key in ('CursorRefs','OwnerPins','QueryResults','ReservedBytes','DecodeTasks'))
    assert all(owner['cache'][key]==0 for key in ('InFlight','CompileReservedBytes','UncachedBytes'))
    assert all(owner[key]['LiveBytes']==0 for key in ('aggregate','sort','native'))


def main(out):
    results=[];summary=[]
    for folder in sorted(p.parent for p in out.glob('*/run.json')):
        run=json.loads((folder/'run.json').read_text())
        assert run['exit_code']==0 and run['log_sha256']==sha(folder/'run.log')
        assert run['observations_sha256']==sha(folder/'observations.jsonl')
        rows=[json.loads(line) for line in (folder/'observations.jsonl').read_text().splitlines()]
        fixture=next(r for r in rows if r['phase']=='fixture');n=fixture['groups']
        assert fixture['input_rows']==sum(2*(1+g%3) for g in range(n))
        request=next(r for r in rows if r['phase']=='request')
        assert request['oracle_sha256']==sha(folder/'oracle.json')
        oracle=[{'service':f's{g:06d}','count':rank} for rank in (3,2,1) for g in range(n) if 1+g%3==rank]
        assert json.loads((folder/'oracle.json').read_text())==oracle
        exports=[r for r in rows if r['phase']=='export'];recoveries=[r for r in rows if r['phase']=='recovery']
        assert len(exports)==len(recoveries)
        for recovery in recoveries:assert recovery['success'];idle(recovery['owners'])
        if exports[0]['success']:assert len(exports)==1
        else:assert len(exports)==2 and all(not r['success'] and r['grpc_code']=='ResourceExhausted' for r in exports)
        for row in exports:
            idle(row['before']);idle(row['after'])
            row['run']=folder.name;row['output_groups']=n;row['input_rows']=fixture['input_rows']
            row['owner_deltas']={owner:{k:v-row['before'][owner][k] for k,v in row['after'][owner].items() if type(v) is int}
                for owner in ('aggregate','sort','native','batch')}
            if row['success']:
                output=folder/f"output-{row['attempt']}.json"
                assert row['decoded_output_sha256']==sha(output) and json.loads(output.read_text())==oracle
                assert row['decoded_rows']==n and row['decoded_output_bytes']==output.stat().st_size
                assert row['owner_deltas']['sort']['K']==0 and row['owner_deltas']['sort']['OutputRows']==n
                assert row['owner_deltas']['sort']['PeakCandidates']==n
            results.append(row)
        cpu=lambda name:{k:int(v) for k,v in (line.split() for line in (folder/name).read_text().splitlines())}
        before,after=cpu('cpu-before.txt'),cpu('cpu-after.txt')
        summary.append({'run':folder.name,'resources':run['resources'],'load_before':run['load_before'],'load_after':run['load_after'],
            'cgroup_peak_bytes':int((folder/'memory-peak.txt').read_text()),'cpu_delta':{k:after[k]-before[k] for k in before},
            'input_rows':fixture['input_rows'],'output_groups':n,'accepted':exports[0]['success'],'attempts':len(exports)})
        if folder.name.startswith('distributed'):
            workers=next(r for r in rows if r['phase']=='worker_visibility')
            assert len(workers['workers'])==2 and all(v>0 for v in workers['workers'].values())
            assert sum(workers['workers'].values())==fixture['input_rows']
    stop=json.loads((out/'stop.json').read_text())
    attempted=sorted(x['output_groups'] for x in summary if x['run'].startswith('standalone'))
    boundary=stop['first_reproducible_boundary_output_rows']
    assert attempted==[n for n in (1000,10000,100000) if boundary is None or n<=boundary]
    if boundary is not None:assert not next(x for x in summary if x['run']==f'standalone-{boundary}')['accepted']
    for name,value in (('attempts.json',results),('runs.json',summary)):(out/name).write_text(json.dumps(value,indent=2)+'\n')
    print(f'Validated {len(summary)} processes, {len(results)} full-export attempts and matching recovery calls')


if __name__=='__main__':
    parser=argparse.ArgumentParser();parser.add_argument('out',type=Path);main(parser.parse_args().out)
