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
"""Run one frozen distributed P3 process; no automatic retries."""
import hashlib
import json
import subprocess
import sys
import time
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
IMAGE = 'banyandb-distributed-querybench:go1.26.10'

def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()

def call(args):
    return subprocess.check_output(args, cwd=ROOT, text=True).strip()

def identity():
    names = ['go.mod', 'go.sum']
    for subtree in ['api', 'banyand', 'pkg']:
        names += sorted(str(path.relative_to(ROOT)) for path in (ROOT/subtree).rglob('*.go'))
    names += ['verification/dpo85bench/p3distributed/' + name for name in ['distributed_test.go', 'protocol.md', 'run.py']]
    return {'head': call(['git', 'rev-parse', 'HEAD']), 'source_hashes': {name: sha(ROOT/name) for name in names if (ROOT/name).is_file()},
            'binary_sha256': sha(HERE/'local/bench.test'), 'go': call(['go','version']),
            'image_id': json.loads(call(['docker','image','inspect',IMAGE]))[0]['Id']}

label = sys.argv[1]
if label not in ['qualification', 'profile', 'round-0', 'round-1', 'round-2']:
    raise SystemExit('invalid fixed protocol label')
out = HERE/'evidence'
out.mkdir(exist_ok=True)
manifest = out/'manifest.json'
current = identity()
if manifest.exists():
    assert json.loads(manifest.read_text()) == current, 'frozen identity changed'
else:
    manifest.write_text(json.dumps(current,indent=2)+'\n')
log = out/(label+'.log')
assert not log.exists(), 'refuse overwrite/retry'
count = 1 if label == 'qualification' else 100 if label == 'profile' else 30
args = ['docker','run','--rm','--name','ql-p3-distributed-'+label,'--cpuset-cpus=0-3','--cpus=4','--memory=8g','--memory-swap=8g',
        '-v',str(HERE)+':/evidence','-e','GOMAXPROCS=4','-e','P3_MEMORY='+('1' if label=='qualification' else '0')]
if label=='profile':
    args += ['-e','P3_PROFILE_DIR=/evidence/evidence']
args += [IMAGE,'sh','-c',f'/evidence/local/bench.test -test.run=\'^$\' -test.bench=\'^BenchmarkPatternsRPC$\' -test.benchtime={count}x -test.count=1 -test.timeout=10m; code=$?; cat /sys/fs/cgroup/cpu.stat; cat /sys/fs/cgroup/memory.peak; exit "$code"']
load = Path('/proc/loadavg').read_text().strip()
started=time.time()
observed=None
with log.open('w') as stream:
    proc=subprocess.Popen(args,stdout=stream,stderr=subprocess.STDOUT)
    for _ in range(40):
        result=subprocess.run(['docker','inspect','ql-p3-distributed-'+label],capture_output=True,text=True)
        if result.returncode==0:
            observed=json.loads(result.stdout)[0]['HostConfig']
            break
        if proc.poll() is not None:
            break
        time.sleep(.1)
    code=proc.wait()
if observed is not None:
    assert observed['NanoCpus'] == 4000000000
    assert observed['Memory'] == 8*1024**3 and observed['MemorySwap'] == 8*1024**3
    assert observed['CpusetCpus'] == '0-3'
record={'command':args,'exit_code':code,'elapsed_seconds':time.time()-started,'host_load_before':load,'host_load_after':Path('/proc/loadavg').read_text().strip(),
        'docker_host_config':observed,'log_sha256':sha(log),'role_cpu_ns':None,'role_reservation_bytes':None,'role_metric_limit':'All server instances and client share one Go process.'}
(out/(label+'-run.json')).write_text(json.dumps(record,indent=2)+'\n')
assert identity()==current, 'identity changed during run'
assert code==0, f'failed process retained: {code}'
