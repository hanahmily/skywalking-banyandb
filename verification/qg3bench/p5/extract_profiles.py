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
"""Render CPU and baseline-subtracted allocation profiles without rerunning RPCs."""
import argparse
from pathlib import Path
import subprocess

parser=argparse.ArgumentParser()
parser.add_argument('out',type=Path)
parser.add_argument('binary',type=Path)
args=parser.parse_args()
for cpu in sorted(args.out.glob('profile*/*-cpu.pprof')):
    stem=cpu.name.removesuffix('-cpu.pprof')
    for kind in ('cpu','alloc'):
        for cumulative in (False,True):
            command=['go','tool','pprof','-top','-nodecount=0','-nodefraction=0','-edgefraction=0']
            if cumulative:command+=['-cum']
            if kind=='alloc':command+=['-sample_index=alloc_space','-base',str(cpu.parent/(stem+'-before.pprof'))]
            command += [str(args.binary),str(cpu if kind=='cpu' else cpu.parent/(stem+'-after.pprof'))]
            result=subprocess.run(command,text=True,capture_output=True,check=True)
            (cpu.parent/(stem+'-'+kind+('-cum' if cumulative else '-flat')+'.txt')).write_text(result.stdout+result.stderr)
