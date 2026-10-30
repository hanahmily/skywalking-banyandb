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
"""Independent integer/decimal arithmetic for the generated thirteen-row witness."""
from decimal import Decimal, localcontext
import json
from pathlib import Path

with localcontext() as context:
    context.prec = 60
    coefficients = {'a': [999999999999999999]*10, 'b': [-999999999999999999, -1], 'c': [None]}
    totals = {key: (sum(v for v in values if v is not None) if any(v is not None for v in values) else None) for key,values in coefficients.items()}
    values = {key: format(Decimal(value)/Decimal(10)**10, '.10f') if value is not None else None for key,value in totals.items()}
    assert values == {'a':'999999999.9999999990','b':'-100000000.0000000000','c':None}
    assert totals['a'] > 2**63-1
    all_rows = format(Decimal(sum(v for v in totals.values() if v is not None))/Decimal(10)**10, '.10f')
    assert all_rows == '899999999.9999999990'
    record = {'source':'independent Python arbitrary-precision integer and decimal arithmetic; no engine result used',
              'coefficients':coefficients,'scale':10,'group_coefficients':totals,'group_strings':values,'scalar_string':all_rows,
              'group_a_exceeds_int64':True,'precision38_maximum':str(10**38-1),'precision38_overflow':str(10**38)}
Path(__file__).with_name('independent-oracle.json').write_text(json.dumps(record,indent=2)+'\n')
