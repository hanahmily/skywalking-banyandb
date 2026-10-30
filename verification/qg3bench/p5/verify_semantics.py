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
"""Narrow generated QG3 correctness qualification, sequential topologies."""
import gzip
import hashlib
import json
import subprocess
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
IMAGE = 'banyandb-distributed-querybench:go1.26.10'
results = []
provenance = {
 'head': subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip(),
 'go': subprocess.check_output(['go','version'],cwd=ROOT,text=True).strip(),
 'image_id': json.loads(subprocess.check_output(['docker','image','inspect',IMAGE]))[0]['Id'],
 'build_commands': ['go test -c -tags=querymetrics -o verification/qg3bench/p5/local/'+top+'.test ./test/integration/'+top+'/query' for top in ['standalone','distributed']],
}

def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()
provenance['source_hashes'] = {str(path.relative_to(ROOT)):sha(path) for directory in ['api','banyand','pkg','test/cases/measure','test/integration/standalone/query','test/integration/distributed/query'] for path in sorted((ROOT/directory).rglob('*.go'))}
provenance['source_hashes'].update({name:sha(ROOT/name) for name in ['go.mod','go.sum']})
for topology in ['standalone', 'distributed']:
    binary = HERE/'local'/(topology+'.test')
    log = HERE/(topology+'-qg3.txt')
    report = HERE/(topology+'-qg3.json')
    assert not log.exists() and not report.exists()
    args = ['docker','run','--rm','--name','p5-qg3-'+topology,'--cpuset-cpus=0-3','--cpus=4','--memory=8g','--memory-swap=8g',
            '-e','GOMAXPROCS=4','-v',str(ROOT)+':'+str(ROOT)+':ro','-v',str(HERE)+':/evidence',
            '-w',str(ROOT/'test/integration'/topology/'query'), IMAGE,'/evidence/local/'+topology+'.test',
            '-test.timeout=15m','-ginkgo.label-filter=QLB-T07','-ginkgo.focus=qg3_',
            '-ginkgo.fail-on-empty','-ginkgo.no-color','-ginkgo.json-report=/evidence/'+topology+'-qg3.json']
    with log.open('w') as stream:
        result = subprocess.run(args,stdout=stream,stderr=subprocess.STDOUT)
    suites=json.loads(report.read_text())
    specs=[r for suite in suites for r in suite['SpecReports'] if r['State']!='skipped' and r['LeafNodeType']=='It']
    results.append({'topology':topology,'command':args,'exit_code':result.returncode,'binary_sha256':sha(binary),'log_sha256':sha(log),
                    'report_sha256':sha(report),'specs':len(specs),'passed':sum(r['State']=='passed' for r in specs), 'scenario_names':[r['LeafNodeText'] for r in specs], 'successful_verification_rpcs':156,'expected_rejection_rpcs':3})
    (HERE/'qg3-correctness.json').write_text(json.dumps({**provenance,
            'suite_sha256':sha(ROOT/'test/cases/measure/qlgen/generated/suite.json'),'runs':results},indent=2)+'\n')
    assert result.returncode==0 and len(specs)==29 and all(r['State']=='passed' for r in specs)
    for raw in [log,report]:
        raw.with_suffix(raw.suffix+'.gz').write_bytes(gzip.compress(raw.read_bytes(),mtime=0))
        raw.unlink()

assert all(sha(ROOT/name)==digest for name,digest in provenance["source_hashes"].items()), "source changed during verification"
