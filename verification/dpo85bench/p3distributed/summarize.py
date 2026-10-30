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
"""Summarize retained process samples without pooling RPCs across processes."""
import hashlib
import json
import re
from pathlib import Path

HERE = Path(__file__).resolve().parent / 'evidence'
samples, memory, traces, costs = [], [], [], []
for file in sorted(HERE.glob('*.log')):
    if file.name.startswith('initial'):
        continue
    arm = None
    for line in file.read_text().splitlines():
        if 'P3_LATENCY ' in line:
            data = json.loads(line.split('P3_LATENCY ', 1)[1])
            data['process'] = file.stem
            samples.append(data)
            arm = data['arm']
        elif 'P3_MEMORY ' in line:
            memory.append(json.loads(line.split('P3_MEMORY ', 1)[1]))
        elif line.startswith('P3_TRACE '):
            label, response = line.split(' response=', 1)
            traces.append({'process': file.stem, 'arm': label.split('arm=')[1], 'response': json.loads(response)})
        if re.match(r'^\s+30\s+', line):
            fields = line.split()
            costs.append({'process': file.stem, 'arm': arm, 'requests': int(fields[0]), **{fields[i+1]: float(fields[i]) for i in range(1,len(fields),2)}})
for name, data in [('samples.json', samples), ('memory.json', memory), ('traces.json', traces), ('costs.json', costs)]:
    (HERE / name).write_text(json.dumps(data, indent=2) + '\n')
retained = [r for r in samples if r['process'].startswith('round') and len(r['latency_ns']) == 30]
assert len(retained) == 9 and len(costs) == 9 and len(memory) == 3
for row in costs:
    assert row['native-rows/op'] == (2 if row['arm'] == 'exact_control' else 16)
for path in HERE.glob('*-run.json'):
    run = json.loads(path.read_text())
    assert run['exit_code'] == 0
    log = HERE / path.name.replace('-run.json', '.log')
    assert hashlib.sha256(log.read_bytes()).hexdigest() == run['log_sha256']
print(json.dumps([{'arm': r['arm'], 'process': r['process'], 'p50_ms': r['p50_ns']/1e6, 'p95_ms': r['p95_ns']/1e6} for r in retained], indent=2))
