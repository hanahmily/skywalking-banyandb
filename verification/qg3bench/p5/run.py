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
"""Frozen default-policy and base/current standalone follow-up."""
import argparse
import datetime
import hashlib
import json
import io
import tarfile
from pathlib import Path
import shutil
import subprocess
import time

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
BASE = Path('/tmp/p5-base')
IMAGE = 'banyandb-distributed-querybench:go1.26.10'
REVISIONS = {'current':'a948c94f2caafd26ff76fb6ec51eb16ff8c9f1c1','base':'39d33bc5d8b49ea7ee33c253de2b476648637669'}


def call(args, cwd=ROOT):
    return subprocess.check_output(args,cwd=cwd,text=True).strip()


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def write(path, obj):
    path.write_text(json.dumps(obj,indent=2)+'\n')


def source(root):
    # All production Go inputs, modules, generated API, and this exact harness.
    paths=[]
    for directory in ('api','banyand','pkg','test','third_party'):
        paths.extend((root/directory).rglob('*.go'))
    paths += [root/'go.mod',root/'go.sum',root/'verification/qg3bench/p5/campaign_test.go',root/'verification/qg3bench/p5/fixture_test.go']
    return {str(p.relative_to(root)):sha(p) for p in sorted(paths) if p.is_file()}


def build(out):
    out.mkdir(parents=True,exist_ok=True)
    if (out/'manifest.json').exists():raise SystemExit('frozen manifest exists')
    assert call(['git','rev-parse','HEAD'])==REVISIONS['current']
    archive = subprocess.check_output(['git','archive',REVISIONS['base']],cwd=ROOT)
    if not BASE.exists():
        BASE.mkdir()
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
            tar.extractall(BASE,filter='data')
        for generated in (ROOT/'api/proto').rglob('*.go'):
            destination=BASE/generated.relative_to(ROOT)
            destination.parent.mkdir(parents=True,exist_ok=True)
            shutil.copyfile(generated,destination)
    # Verify archive tracked inputs were not replaced by current production code.
    with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
        for entry in tar:
            if entry.isfile() and (entry.name.endswith('.go') or entry.name in ('go.mod','go.sum')):
                assert (BASE/entry.name).read_bytes()==tar.extractfile(entry).read(), entry.name
    (BASE/'verification/qg3bench/p5').mkdir(parents=True,exist_ok=True)
    assert not call(['git','diff','HEAD','--name-only','--','api','banyand','pkg','test','go.mod','go.sum'])
    # The proto schemas are unchanged across revisions; generated code is identical.
    assert not call(['git','diff',REVISIONS['base'],REVISIONS['current'],'--name-only','--','api/proto'])
    for name in ('campaign_test.go','fixture_test.go'):
        shutil.copyfile(HERE/name,BASE/'verification/qg3bench/p5'/name)
    before={key:source(root) for key,root in (('base',BASE),('current',ROOT))}
    for key,root in (('base',BASE),('current',ROOT)):
        subprocess.run(['go','test','-c','-tags=querymetrics','-o',str(out/(key+'.test')),'./verification/qg3bench/p5'],cwd=root,check=True)
        assert before[key]==source(root)
    manifest={'revisions':REVISIONS,'sources':before,'go':call(['go','version']),
              'image_id':json.loads(call(['docker','image','inspect',IMAGE]))[0]['Id'],
              'binary_sha256':{key:sha(out/(key+'.test')) for key in REVISIONS},
              'harness_sha256':{name:sha(HERE/name) for name in ('campaign_test.go','fixture_test.go')},'runner_sha256':sha(Path(__file__)),
              'protocol_sha256':sha(HERE/'protocol.md'),
              'base_time':(datetime.datetime.now(datetime.timezone.utc)-datetime.timedelta(hours=1)).replace(minute=0,second=0,microsecond=0).isoformat().replace('+00:00','Z')}
    write(out/'manifest.json',manifest)


def verify(out):
    m=json.loads((out/'manifest.json').read_text())
    assert m['runner_sha256']==sha(Path(__file__)) and m['protocol_sha256']==sha(HERE/'protocol.md')
    for key,root in (('base',BASE),('current',ROOT)):
        assert m['sources'][key]==source(root), 'source changed: '+key
        assert m['binary_sha256'][key]==sha(out/(key+'.test'))
    assert m['image_id']==json.loads(call(['docker','image','inspect',IMAGE]))[0]['Id']


def run(out,mode,size,round_no=0,revision='current',bypass=False):
    label=f'{mode}-{size}-{round_no}-{revision}-'+('bypass' if bypass else 'cache')
    dest=out/label
    if dest.exists():
        if mode in ('qualify','memory','profile','profile_cache') and successful(out,label):
            print('preserved successful prerequisite '+label,flush=True);return
        raise SystemExit('refuse repeat '+label)
    dest.mkdir()
    manifest=json.loads((out/'manifest.json').read_text())
    shell=f'cat /sys/fs/cgroup/cpu.stat > /evidence/{label}/cpu-before.txt; '
    shell+=f'/evidence/{revision}.test -test.run=^TestDecimalRPC$ -test.v -test.timeout=10m; code=$?; '
    shell+=f'cat /sys/fs/cgroup/cpu.stat > /evidence/{label}/cpu-after.txt; '
    shell+=f'cat /sys/fs/cgroup/memory.peak > /evidence/{label}/memory-peak.txt; '
    shell+=f'chmod a+r /evidence/{label}/*; exit "$code"'
    args=['docker','run','--rm','--name','p5-'+label,'--cpuset-cpus=0,1','--cpus=2','--memory=4g','--memory-swap=4g',
          '-v',str(ROOT)+':'+str(ROOT)+':ro','-v',str(out)+':/evidence','-w',str(ROOT),'-e','GOMAXPROCS=2',
          '-e',f'QG3_ROWS={size}','-e',f'P5_ROUND={round_no}',
          '-e','P5_BASE_TIME='+manifest['base_time'],'-e','P5_OUTPUT=/evidence/'+label+'/observations.jsonl',
          '-e','P5_REVISION='+revision,'-e','P5_MODE='+mode,'-e','P5_BYPASS='+('1' if bypass else '0'),IMAGE,'sh','-c',shell]
    started=time.monotonic();load=Path('/proc/loadavg').read_text().strip();observed=None
    with (dest/'run.log').open('w') as stream:
        proc=subprocess.Popen(args,stdout=stream,stderr=subprocess.STDOUT)
        for _ in range(100):
            data=subprocess.run(['docker','inspect','p5-'+label],capture_output=True,text=True)
            if data.returncode==0:
                info=json.loads(data.stdout)[0]
                observed={k:info['HostConfig'][k] for k in ('NanoCpus','Memory','MemorySwap','CpusetCpus')}
                observed['image_id']=info['Image'];break
            if proc.poll() is not None:break
            time.sleep(.1)
        code=proc.wait()
    record={'label':label,'command':args,'resources':observed,'exit_code':code,'seconds':time.monotonic()-started,
            'load_before':load,'load_after':Path('/proc/loadavg').read_text().strip(),'log_sha256':sha(dest/'run.log'),
            'observations_sha256':sha(dest/'observations.jsonl') if (dest/'observations.jsonl').exists() else None}
    write(dest/'run.json',record);print(json.dumps({k:v for k,v in record.items() if k!='command'}),flush=True)
    assert observed and observed['Memory']==4*1024**3 and observed['MemorySwap']==4*1024**3 and observed['NanoCpus']==2*10**9
    assert observed['CpusetCpus']=='0,1' and observed['image_id']==manifest['image_id']
    if code:raise SystemExit('failed run '+label)


def successful(out,label):
    dest=out/label
    record=json.loads((dest/'run.json').read_text())
    assert record['exit_code']==0 and record['log_sha256']==sha(dest/'run.log')
    manifest=json.loads((out/'manifest.json').read_text())
    digest=record.get('observations_sha256') or manifest.get('preserved_evidence',{}).get(label)
    assert digest and digest==sha(dest/'observations.jsonl')
    rows=[json.loads(line) for line in (dest/'observations.jsonl').read_text().splitlines()]
    mode=label.split('-')[0]
    selected=[r for r in rows if r['phase']==('profile' if mode=='profile_cache' else mode)]
    if mode=='qualify':
        assert len(selected)==4 and all(r['success'] for r in selected)
    elif mode=='memory':
        assert len(selected)==4 and all(r['requests']==1 and r['memory_after']['LiveReservedBytes']==0 for r in selected)
    elif mode=='profile':
        assert {r['arm'] for r in selected}=={'scalar_int','scalar_decimal'}
        assert all(r['requests']==1000 for r in selected)
    elif mode=='profile_cache':
        assert len(selected)==1 and selected[0]['arm']=='scalar_decimal' and selected[0]['requests']==1000
    return True


def prepare(out):
    for size in (64,8192):
        run(out,'qualify',size)
        successful(out,f'qualify-{size}-0-current-cache')
        run(out,'memory',size)



def timing(out):
    for size in (64,8192):
        successful(out,f'qualify-{size}-0-current-cache')
        successful(out,f'memory-{size}-0-current-cache')
    for size in (64,8192):
        for index in range(5):
            for revision in (('base','current') if index%2==0 else ('current','base')):
                run(out,'timing',size,index,revision)
    for index in range(5):
        for bypass in ((False,True) if index%2==0 else (True,False)):
            run(out,'cache',64,index,bypass=bypass)


def profiles(out):
    decision_path=out/'profile-decision.json'
    if decision_path.exists():raise SystemExit('profile decision already frozen; do not repeat')
    differences=[]
    for index in range(5):
        label=f'timing-64-{index}-current-cache'
        dest=out/label;record=json.loads((dest/'run.json').read_text())
        assert record['exit_code']==0 and record['observations_sha256']==sha(dest/'observations.jsonl')
        rows={r['arm']:r for r in [json.loads(line) for line in (dest/'observations.jsonl').read_text().splitlines()] if r['phase']=='timing'}
        differences.append({key:rows['scalar_decimal'][key]-rows['scalar_int'][key] for key in ('p50_ns','allocated_bytes_per_rpc')})
    trigger=all(d['p50_ns']>0 for d in differences) or all(d['allocated_bytes_per_rpc']>0 for d in differences)
    memory=[json.loads(line) for line in (out/'memory-8192-0-current-cache/observations.jsonl').read_text().splitlines()]
    group=next(r for r in memory if r['phase']=='memory' and r['arm']=='group_decimal')
    state_capacity=group['aggregate_after']['StateCapacity']-group['aggregate_before']['StateCapacity']
    share=state_capacity/group['memory_after']['PeakReservedBytes']
    decision={'complete_rounds':5,'small_scalar_differences':differences,'profile_triggered':trigger,
              'group_state_capacity_bytes':state_capacity,'group_peak_reserved_bytes':group['memory_after']['PeakReservedBytes'],
              'group_state_capacity_share':share,'high_group_trigger':0.10,'high_group_needed':share>=0.10,
              'reason':'Profile if all five64-row scalar differences positive for p50 or allocated bytes; high-group diagnostic only when state capacity reaches10% of tracked peak, not an SLO.'}
    write(decision_path,decision)
    if decision['high_group_needed']:raise SystemExit('state share merits separately declared bounded high-group diagnostic')
    if trigger:
        run(out,'profile',64)
        run(out,'profile_cache',64)
        run(out,'profile_cache',64,bypass=True)


if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('action',choices=['build','prepare','timing','profiles']);p.add_argument('--out',type=Path,default=HERE/'local')
    a=p.parse_args();out=a.out.resolve()
    if a.action=='build':build(out)
    else:
        verify(out)
        {'prepare':prepare,'timing':timing,'profiles':profiles}[a.action](out)
        verify(out)
