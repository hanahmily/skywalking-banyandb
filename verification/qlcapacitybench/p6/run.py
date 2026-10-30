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
"""Frozen P6 full-export feasibility sweep; never repeat existing stages."""
import argparse
import datetime
import hashlib
import json
from pathlib import Path
import subprocess
import time

HERE=Path(__file__).resolve().parent
ROOT=HERE.parents[2]
IMAGE='banyandb-distributed-querybench:go1.26.10'
REVISION='1af21ec71f2b98bae44371e84e279b58929cc200'


def call(args):return subprocess.check_output(args,cwd=ROOT,text=True).strip()
def sha(path):return hashlib.sha256(path.read_bytes()).hexdigest()
def write(path,value):path.write_text(json.dumps(value,indent=2)+'\n')
def source():
    paths=[]
    for directory in ('api','banyand','pkg','test','third_party'):
        paths.extend((ROOT/directory).rglob('*.go'))
    paths += [ROOT/'go.mod',ROOT/'go.sum',*HERE.glob('*.go')]
    return {str(p.relative_to(ROOT)):sha(p) for p in sorted(paths) if p.is_file()}


def build(out):
    out.mkdir(parents=True,exist_ok=True)
    if (out/'manifest.json').exists():raise SystemExit('frozen manifest exists')
    assert call(['git','rev-parse','HEAD'])==REVISION
    assert not call(['git','diff','HEAD','--name-only','--','api','banyand','pkg','test','go.mod','go.sum'])
    before=source()
    subprocess.run(['go','test','-c','-tags=querymetrics','-o',str(out/'current.test'),'./verification/qlcapacitybench/p6'],cwd=ROOT,check=True)
    assert before==source()
    write(out/'manifest.json',{'revision':REVISION,'sources':before,'go':call(['go','version']),
        'image_id':json.loads(call(['docker','image','inspect',IMAGE]))[0]['Id'],
        'binary_sha256':sha(out/'current.test'),'runner_sha256':sha(Path(__file__)),
        'protocol_sha256':sha(HERE/'protocol.md'),
        'base_time':(datetime.datetime.now(datetime.timezone.utc)-datetime.timedelta(hours=1)).replace(minute=0,second=0,microsecond=0).isoformat().replace('+00:00','Z')})


def verify(out):
    m=json.loads((out/'manifest.json').read_text())
    assert m['sources']==source() and m['binary_sha256']==sha(out/'current.test')
    assert m['runner_sha256']==sha(Path(__file__)) and m['protocol_sha256']==sha(HERE/'protocol.md')
    assert m['image_id']==json.loads(call(['docker','image','inspect',IMAGE]))[0]['Id']


def run(out,size,distributed=False):
    label=('distributed' if distributed else 'standalone')+'-'+str(size)
    dest=out/label
    if dest.exists():raise SystemExit('refuse repeat '+label)
    dest.mkdir();m=json.loads((out/'manifest.json').read_text())
    shell=f'cat /sys/fs/cgroup/cpu.stat > /evidence/{label}/cpu-before.txt; '
    shell+='/evidence/current.test -test.run=^TestExportRPC$ -test.v -test.timeout=12m; code=$?; '
    shell+=f'cat /sys/fs/cgroup/cpu.stat > /evidence/{label}/cpu-after.txt; '
    shell+=f'cat /sys/fs/cgroup/memory.peak > /evidence/{label}/memory-peak.txt; '
    shell+=f'chmod a+r /evidence/{label}/*; exit "$code"'
    cpus,memory=('4','8g') if distributed else ('2','4g')
    args=['docker','run','--rm','--name','p6-'+label,'--cpuset-cpus='+('0,1,2,3' if distributed else '0,1'),
        '--cpus='+cpus,'--memory='+memory,'--memory-swap='+memory,
        '-v',str(ROOT)+':'+str(ROOT)+':ro','-v',str(out)+':/evidence','-w',str(ROOT),'-e','GOMAXPROCS='+cpus,
        '-e','P6_ROWS='+str(size),'-e','P6_BASE_TIME='+m['base_time'],'-e','P6_OUTPUT=/evidence/'+label+'/observations.jsonl',
        '-e','P6_DISTRIBUTED='+('1' if distributed else '0'),IMAGE,'sh','-c',shell]
    start=time.monotonic();load=Path('/proc/loadavg').read_text().strip();observed=None
    with (dest/'run.log').open('w') as stream:
        proc=subprocess.Popen(args,stdout=stream,stderr=subprocess.STDOUT)
        for _ in range(100):
            info=subprocess.run(['docker','inspect','p6-'+label],text=True,capture_output=True)
            if info.returncode==0:
                data=json.loads(info.stdout)[0]
                observed={k:data['HostConfig'][k] for k in ('NanoCpus','Memory','MemorySwap','CpusetCpus')}
                observed['image_id']=data['Image'];break
            if proc.poll() is not None:break
            time.sleep(.1)
        code=proc.wait()
    record={'label':label,'command':args,'resources':observed,'exit_code':code,'seconds':time.monotonic()-start,
        'load_before':load,'load_after':Path('/proc/loadavg').read_text().strip(),'log_sha256':sha(dest/'run.log'),
        'observations_sha256':sha(dest/'observations.jsonl') if (dest/'observations.jsonl').exists() else None}
    write(dest/'run.json',record);print(json.dumps({k:v for k,v in record.items() if k!='command'}),flush=True)
    assert observed and observed['Memory']==int(cpus)*2*1024**3 and observed['MemorySwap']==observed['Memory']
    assert observed['NanoCpus']==int(cpus)*10**9 and observed['image_id']==m['image_id']
    if code:raise SystemExit('failed run '+label)
    return successful(out,label)


def successful(out,label):
    dest=out/label;record=json.loads((dest/'run.json').read_text())
    assert record['exit_code']==0 and record['log_sha256']==sha(dest/'run.log')
    assert record['observations_sha256']==sha(dest/'observations.jsonl')
    rows=[json.loads(line) for line in (dest/'observations.jsonl').read_text().splitlines()]
    exports=[r for r in rows if r['phase']=='export'];recovery=[r for r in rows if r['phase']=='recovery']
    assert len(exports)==len(recovery) and all(r['success'] for r in recovery)
    if exports[0]['success']:
        assert len(exports)==1
        return True
    assert len(exports)==2 and all(not r['success'] and r['grpc_code']=='ResourceExhausted' for r in exports)
    assert exports[0]['boundary']==exports[1]['boundary']
    return False


def sweep(out):
    assert successful(out,'distributed-1000'),'distributed small qualification rejected'
    for size in (1000,10000,100000):
        accepted=run(out,size)
        if not accepted:
            write(out/'stop.json',{'first_reproducible_boundary_output_rows':size,'larger_sizes_not_attempted':[n for n in (1000,10000,100000) if n>size]})
            return
    write(out/'stop.json',{'first_reproducible_boundary_output_rows':None,'largest_verified_output_rows':100000})


if __name__=='__main__':
    parser=argparse.ArgumentParser();parser.add_argument('action',choices=['build','qualify','sweep']);parser.add_argument('--out',type=Path,default=HERE/'local')
    args=parser.parse_args();out=args.out.resolve()
    if args.action=='build':build(out)
    else:
        verify(out)
        if args.action=='qualify':assert run(out,1000,distributed=True)
        else:sweep(out)
        verify(out)
