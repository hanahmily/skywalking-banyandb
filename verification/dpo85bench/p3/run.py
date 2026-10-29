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
"""Fixed current-source LIKE/ILIKE RPC and isolated ownership campaign."""
import argparse
import datetime
import hashlib
import json
from pathlib import Path
import re
import statistics
import subprocess
import time

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
IMAGE = 'banyandb-distributed-querybench:go1.26.10'
ARMS = {'LIKE', 'ILIKE', 'IN', 'LIKE_no_match', 'ILIKE_no_match', 'IN_no_match', 'exact_control'}


def call(args):
    return subprocess.check_output(args, cwd=ROOT, text=True).strip()


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def write(path, data):
    path.write_text(json.dumps(data, indent=2) + '\n')


def source():
    names = call(['git', 'ls-files', '-co', '--exclude-standard']).splitlines()
    return {name: sha(ROOT / name) for name in sorted(set(names))
            if (ROOT / name).is_file() and (name.endswith('.go') or name in ('go.mod', 'go.sum'))}


def identity():
    return {'revision': call(['git', 'rev-parse', 'HEAD']), 'sources': source(),
            'go': call(['go', 'version']), 'runner_sha256': sha(Path(__file__)),
            'protocol_sha256': sha(HERE / 'protocol.md'),
            'image_id': json.loads(call(['docker', 'image', 'inspect', IMAGE]))[0]['Id']}


def build(out):
    out.mkdir(parents=True, exist_ok=True)
    if (out / 'manifest.json').exists():
        raise SystemExit('refuse to overwrite frozen campaign')
    before = identity()
    subprocess.run(['go', 'test', '-c', '-tags=querymetrics', '-o', str(out / 'bench.test'), './verification/dpo85bench/p3'], cwd=ROOT, check=True)
    if before != identity():
        raise SystemExit('source changed during build; retry build before timing')
    before.update(binary_sha256=sha(out / 'bench.test'), utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),
                  status=call(['git', 'status', '--short']), cpus='0,1', memory_bytes=4*1024**3,
                  swap_bytes=0, gomaxprocs=2, timed_requests=100, rounds=3)
    write(out / 'manifest.json', before)
    (out / 'source.patch').write_text(call(['git', 'diff', '--binary', 'HEAD']))


def verify(out):
    manifest = json.loads((out / 'manifest.json').read_text())
    for key, value in identity().items():
        if manifest[key] != value:
            raise SystemExit('frozen identity changed: ' + key)
    if manifest['binary_sha256'] != sha(out / 'bench.test'):
        raise SystemExit('binary changed')


def run(out, size, round_no, memory=False):
    label = f'{size}-{round_no}-' + ('memory' if memory else 'timing')
    if (out / (label + '.log')).exists():
        raise SystemExit('refuse repeat: ' + label)
    args = ['docker', 'run', '--rm', '--name', 'ql-p3-' + label, '--cpuset-cpus=0,1', '--cpus=2', '--memory=4g', '--memory-swap=4g',
            '-v', str(ROOT) + ':' + str(ROOT) + ':ro', '-v', str(out) + ':/evidence', '-w', str(ROOT),
            '-e', 'GOMAXPROCS=2', '-e', f'PATTERN_BENCH_NAMES={size}', '-e', f'PATTERN_BENCH_ROUND={round_no}',
            '-e', 'P3_MEMORY=' + ('1' if memory else '0'), IMAGE, 'sh', '-c',
            '/evidence/bench.test -test.run=\'^$\' -test.bench=\'^BenchmarkPatternsRPC$\' '
            '-test.benchtime=' + ('1x' if memory else '100x') + ' -test.count=1 -test.timeout=10m; '
            'code=$?; printf "P3_CGROUP_PEAK "; cat /sys/fs/cgroup/memory.peak; exit "$code"']
    load_before = Path('/proc/loadavg').read_text().strip()
    observed = None
    started = time.monotonic()
    with (out / (label + '.log')).open('w') as stream:
        process = subprocess.Popen(args, stdout=stream, stderr=subprocess.STDOUT)
        for _ in range(100):
            inspected = subprocess.run(['docker', 'inspect', 'ql-p3-' + label], capture_output=True, text=True)
            if inspected.returncode == 0:
                info = json.loads(inspected.stdout)[0]
                observed = {key: info['HostConfig'][key] for key in ('NanoCpus', 'Memory', 'MemorySwap', 'CpusetCpus')}
                observed['image_id'] = info['Image']
                break
            if process.poll() is not None:
                break
            time.sleep(.1)
        exit_code = process.wait()
    assert observed is not None
    assert observed['NanoCpus'] == 2000000000 and observed['Memory'] == 4*1024**3 and observed['MemorySwap'] == 4*1024**3 and observed['CpusetCpus'] == '0,1'
    record = {'label': label, 'command': args, 'exit_code': exit_code, 'resources': observed,
              'load_before': load_before, 'load_after': Path('/proc/loadavg').read_text().strip(),
              'wall_seconds': time.monotonic()-started, 'log_sha256': sha(out / (label + '.log'))}
    write(out / (label + '-run.json'), record)
    print(json.dumps(record | {'command': '(see run JSON)'}), flush=True)
    if exit_code:
        raise SystemExit('failed run: ' + label)


def summarize(out):
    samples, probes, process = [], [], []
    for size in (1000, 10000):
        for memory, rounds in ((False, range(3)), (True, range(1))):
            for round_no in rounds:
                label = f'{size}-{round_no}-' + ('memory' if memory else 'timing')
                record = json.loads((out / (label + '-run.json')).read_text())
                log = (out / (label + '.log')).read_text()
                assert record['exit_code'] == 0 and record['log_sha256'] == sha(out / (label + '.log'))
                assert '\nPASS\n' in log
                rows = []
                for line in log.splitlines():
                    match = re.match(r'BenchmarkPatternsRPC/names_(\d+)/(\w+)-2\s+(\d+)\s+(.+)', line)
                    if match:
                        fields = match[4].split()
                        row = {'names': int(match[1]), 'arm': match[2], 'iterations': int(match[3]), 'round': round_no}
                        row.update({fields[i+1]: float(fields[i]) for i in range(0, len(fields), 2)})
                        rows.append(row)
                    if 'P3_MEMORY ' in line:
                        probes.append(json.loads(line.split('P3_MEMORY ', 1)[1]) | {'names': size})
                # Memory probe output interrupts Go's benchmark line; only timing samples are parsed.
                if not memory:
                    assert {r['arm'] for r in rows} == ARMS and len(rows) == 7
                    assert all(r['iterations'] == 100 for r in rows)
                    assert all(r['native-rows/op'] == (0 if 'no_match' in r['arm'] else 2 if r['arm'] == 'exact_control' else 16) for r in rows)
                    samples.extend(rows)
                peak = re.findall(r'^P3_CGROUP_PEAK (\d+)$', log, re.M)
                assert len(peak) == 1
                process.append({'label': label, 'cgroup_peak_bytes': int(peak[0])})
    assert len(samples) == 42 and len(probes) == 14
    for p in probes:
        assert p['requests'] == 20 and p['owner_pins'] == 0 and p['memory_after']['LiveReservedBytes'] == 0
    write(out / 'samples.json', samples)
    write(out / 'memory.json', probes)
    write(out / 'process-memory.json', process)
    lines = ['| Names | Arm | Process mean latency ms: median [min, max] | CPU ms median | B/op median | allocs/op median | Tracked peak B | Native rows/op |',
             '|---:|---|---:|---:|---:|---:|---:|---:|']
    for size in (1000, 10000):
        for arm in sorted(ARMS):
            group = [r for r in samples if r['names'] == size and r['arm'] == arm]
            lat = [r['ns/op']/1e6 for r in group]
            med = lambda key: statistics.median(r[key] for r in group)
            lines.append(f'| {size} | {arm} | {statistics.median(lat):.3f} [{min(lat):.3f}, {max(lat):.3f}] | {med("cpu-ns/op")/1e6:.3f} | {med("B/op"):.0f} | {med("allocs/op"):.0f} | {med("peak-tracked-B"):.0f} | {med("native-rows/op"):g} |')
    (out / 'table.md').write_text('\n'.join(lines) + '\n')


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('action', choices=['build', 'run', 'summarize'])
    parser.add_argument('--out', type=Path, default=HERE / 'local')
    options = parser.parse_args()
    output = options.out.resolve()
    if options.action == 'build':
        build(output)
    elif options.action == 'run':
        verify(output)
        for names in (1000, 10000):
            run(output, names, 0, memory=True)
        for names in (1000, 10000):
            for index in range(3):
                run(output, names, index)
        verify(output)
        summarize(output)
    else:
        summarize(output)
