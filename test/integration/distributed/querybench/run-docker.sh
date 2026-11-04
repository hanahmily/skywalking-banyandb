#!/usr/bin/env bash
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
# KIND, either express or implied.  See the License for the
# specific language governing permissions and limitations
# under the License.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "${SCRIPT_DIR}/../../../.." && pwd)
IMAGE=${DQB_DOCKER_IMAGE:-banyandb-distributed-querybench:go1.26}
ENGINE=${DQB_ENGINE:-measure}
CPUS=${DQB_CPU_LIMIT:-4}
MEMORY=${DQB_MEMORY_LIMIT:-8g}
MEMORY_SWAP=${DQB_MEMORY_SWAP_LIMIT:-${MEMORY}}
PIDS_LIMIT=${DQB_PIDS_LIMIT:-4096}
BUILD_IMAGE=1
DOCKER_ARGS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --image)
      IMAGE=$2
      shift 2
      ;;
    --cpus)
      CPUS=$2
      shift 2
      ;;
    --memory)
      MEMORY=$2
      MEMORY_SWAP=${DQB_MEMORY_SWAP_LIMIT:-${MEMORY}}
      shift 2
      ;;
    --memory-swap)
      MEMORY_SWAP=$2
      shift 2
      ;;
    --pids-limit)
      PIDS_LIMIT=$2
      shift 2
      ;;
    --pull-image)
      BUILD_IMAGE=0
      shift
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 2
      ;;
  esac
done

# DQB_HOST_WORK_DIR moves the Go caches and the report directory (which also holds the cluster's data files) off the
# repository checkout onto another host directory, mounted at /report. DQB_GO_MOD_CACHE optionally names the host
# module cache to mount instead of ${DQB_HOST_WORK_DIR}/go-cache/pkg.
GO_PKG_CACHE="${REPO_ROOT}/.omx/go-cache/pkg"
GO_BUILD_CACHE="${REPO_ROOT}/.omx/go-cache/build"
GO_BUILD_CACHE_IN_CONTAINER=/work/.omx/go-cache/build
DEFAULT_REPORT_DIR=/work/.omx/bench-reports/distributed-query
if [[ -n "${DQB_HOST_WORK_DIR:-}" ]]; then
  GO_PKG_CACHE="${DQB_GO_MOD_CACHE:-${DQB_HOST_WORK_DIR}/go-cache/pkg}"
  GO_BUILD_CACHE="${DQB_HOST_WORK_DIR}/go-cache/build"
  GO_BUILD_CACHE_IN_CONTAINER=/gocache
  DEFAULT_REPORT_DIR=/report
  mkdir -p "${DQB_HOST_WORK_DIR}/report"
else
  mkdir -p "${REPO_ROOT}/.omx/bench-reports/distributed-query"
fi
mkdir -p "${GO_PKG_CACHE}" "${GO_BUILD_CACHE}"
DEFAULT_SCENARIOS="scan_all,top_with_filter"
DEFAULT_CARDINALITIES="1024,10000,100000,1000000,2000000"
DEFAULT_SHARD_NUM=2
if [[ "${ENGINE}" == "trace" ]]; then
  DEFAULT_SCENARIOS="trace_by_id,trace_tag_filter"
  DEFAULT_CARDINALITIES="1000,10000,100000,1000000,2000000"
fi
if [[ "${ENGINE}" == "stream" ]]; then
  DEFAULT_SCENARIOS="stream_time_window,stream_index_order,stream_filter,stream_traced"
  DEFAULT_CARDINALITIES="10000,100000,1000000"
  DEFAULT_SHARD_NUM=4
fi

if [[ "${BUILD_IMAGE}" == "1" ]]; then
  docker build -t "${IMAGE}" -f "${SCRIPT_DIR}/Dockerfile" "${SCRIPT_DIR}"
else
  docker pull "${IMAGE}"
fi

DOCKER_ARGS+=(--rm)
DOCKER_ARGS+=(--cpus "${CPUS}")
DOCKER_ARGS+=(--memory "${MEMORY}")
DOCKER_ARGS+=(--memory-swap "${MEMORY_SWAP}")
DOCKER_ARGS+=(--pids-limit "${PIDS_LIMIT}")
DOCKER_ARGS+=(-e RUN_DISTRIBUTED_QUERY_BENCH=1)
DOCKER_ARGS+=(-e DQB_IN_CONTAINER=1)
DOCKER_ARGS+=(-e DQB_CPU_LIMIT="${CPUS}")
DOCKER_ARGS+=(-e DQB_MEMORY_LIMIT="${MEMORY}")
DOCKER_ARGS+=(-e DQB_DOCKER_IMAGE="${IMAGE}")
DOCKER_ARGS+=(-e DQB_ENGINE="${ENGINE}")
DOCKER_ARGS+=(-e DQB_TEST_BINARY="${DQB_TEST_BINARY:-}")
DOCKER_ARGS+=(-e DQB_T17_OVERLOAD_REPORT="${DQB_T17_OVERLOAD_REPORT:-}")
DOCKER_ARGS+=(-e DQB_T17_BLOCKED_32_REPORT="${DQB_T17_BLOCKED_32_REPORT:-}")
DOCKER_ARGS+=(-e DQB_MATRIX="${DQB_MATRIX:-A}")
DOCKER_ARGS+=(-e DQB_CARDINALITIES="${DQB_CARDINALITIES:-${DEFAULT_CARDINALITIES}}")
DOCKER_ARGS+=(-e DQB_SCENARIOS="${DQB_SCENARIOS:-${DEFAULT_SCENARIOS}}")
DOCKER_ARGS+=(-e DQB_MODES="${DQB_MODES:-vec}")
DOCKER_ARGS+=(-e DQB_SPANS_PER_TRACE="${DQB_SPANS_PER_TRACE:-20}")
DOCKER_ARGS+=(-e DQB_SPAN_DIST="${DQB_SPAN_DIST:-uniform}")
DOCKER_ARGS+=(-e DQB_FILTER_SELECTIVITY="${DQB_FILTER_SELECTIVITY:-0.01}")
DOCKER_ARGS+=(-e DQB_TRACE_ID_BATCH="${DQB_TRACE_ID_BATCH:-1}")
DOCKER_ARGS+=(-e DQB_SHARD_NUM="${DQB_SHARD_NUM:-${DEFAULT_SHARD_NUM}}")
# Stream engine: the data nodes x replicas matrix and the per-combo knobs.
DOCKER_ARGS+=(-e DQB_STREAM_NODES="${DQB_STREAM_NODES:-}")
DOCKER_ARGS+=(-e DQB_STREAM_REPLICAS="${DQB_STREAM_REPLICAS:-0}")
DOCKER_ARGS+=(-e DQB_STREAM_REPLICAS_LIST="${DQB_STREAM_REPLICAS_LIST:-}")
DOCKER_ARGS+=(-e DQB_STREAM_HEAP_PROBE_QUERIES="${DQB_STREAM_HEAP_PROBE_QUERIES:-8}")
DOCKER_ARGS+=(-e DQB_STREAM_VARIANTS="${DQB_STREAM_VARIANTS:-}")
DOCKER_ARGS+=(-e DQB_STREAM_PAYLOAD_BYTES="${DQB_STREAM_PAYLOAD_BYTES:-512}")
DOCKER_ARGS+=(-e DQB_STREAM_SERIAL_ITERATIONS="${DQB_STREAM_SERIAL_ITERATIONS:-30}")
if [[ -n "${DQB_BASE_NANOS:-}" ]]; then
  DOCKER_ARGS+=(-e DQB_BASE_NANOS="${DQB_BASE_NANOS}")
fi
DOCKER_ARGS+=(-e DQB_DATA_NODES="${DQB_DATA_NODES:-2}")
DOCKER_ARGS+=(-e DQB_SPAN_BYTES="${DQB_SPAN_BYTES:-1024}")
DOCKER_ARGS+=(-e DQB_QUERY_MEMORY_MIB="${DQB_QUERY_MEMORY_MIB:-256}")
DOCKER_ARGS+=(-e DQB_SOAK="${DQB_SOAK:-0}")
# Long-lived write soak (TestTraceWriteSoak, or TestStreamWriteSoak with DQB_WRITE_SOAK_ENGINE=stream).
DOCKER_ARGS+=(-e DQB_WRITE_SOAK_ENGINE="${DQB_WRITE_SOAK_ENGINE:-trace}")
DOCKER_ARGS+=(-e DQB_WRITE_SOAK="${DQB_WRITE_SOAK:-0}")
DOCKER_ARGS+=(-e DQB_WRITE_SOAK_WRITERS="${DQB_WRITE_SOAK_WRITERS:-4}")
DOCKER_ARGS+=(-e DQB_WRITE_SOAK_DURATION="${DQB_WRITE_SOAK_DURATION:-5m}")
DOCKER_ARGS+=(-e DQB_WRITE_SOAK_RATE="${DQB_WRITE_SOAK_RATE:-2000}")
DOCKER_ARGS+=(-e DQB_WRITE_SOAK_LABEL="${DQB_WRITE_SOAK_LABEL:-run}")
# The stream soak checks every element by default; the trace soak samples above 200000 traces.
if [[ "${DQB_WRITE_SOAK_ENGINE:-trace}" == "stream" ]]; then
  DOCKER_ARGS+=(-e DQB_WRITE_SOAK_FULLCHECK_MAX="${DQB_WRITE_SOAK_FULLCHECK_MAX:-100000000}")
else
  DOCKER_ARGS+=(-e DQB_WRITE_SOAK_FULLCHECK_MAX="${DQB_WRITE_SOAK_FULLCHECK_MAX:-200000}")
fi
DOCKER_ARGS+=(-e SOAK_HEAP_GROWTH_MAX_PCT="${SOAK_HEAP_GROWTH_MAX_PCT:-10}")
# Measure soak bounds and its go test timeout (TestMeasureVecSoak).
DOCKER_ARGS+=(-e DQB_MEASURE_SOAK_ITERATIONS="${DQB_MEASURE_SOAK_ITERATIONS:-6000}")
DOCKER_ARGS+=(-e DQB_MEASURE_SOAK_DURATION="${DQB_MEASURE_SOAK_DURATION:-3m}")
DOCKER_ARGS+=(-e DQB_SOAK_TEST_TIMEOUT="${DQB_SOAK_TEST_TIMEOUT:-60m}")
DOCKER_ARGS+=(-e DQB_QUERY_WORKERS="${DQB_QUERY_WORKERS:-4}")
DOCKER_ARGS+=(-e DQB_QUERY_ITERATIONS="${DQB_QUERY_ITERATIONS:-50}")
DOCKER_ARGS+=(-e DQB_TRACE_BY_ID_ITERATIONS="${DQB_TRACE_BY_ID_ITERATIONS:-1000}")
DOCKER_ARGS+=(-e DQB_WARMUP_ITERATIONS="${DQB_WARMUP_ITERATIONS:-3}")
DOCKER_ARGS+=(-e DQB_WRITERS="${DQB_WRITERS:-4}")
DOCKER_ARGS+=(-e DQB_PROFILE="${DQB_PROFILE:-1}")
DOCKER_ARGS+=(-e DQB_REPORT_DIR="${DQB_REPORT_DIR:-${DEFAULT_REPORT_DIR}}")
DOCKER_ARGS+=(-e GOCACHE="${GO_BUILD_CACHE_IN_CONTAINER}")
DOCKER_ARGS+=(-v "${REPO_ROOT}:/work")
DOCKER_ARGS+=(-v "${GO_PKG_CACHE}:/go/pkg/mod")
if [[ -n "${DQB_HOST_WORK_DIR:-}" ]]; then
  DOCKER_ARGS+=(-v "${GO_BUILD_CACHE}:${GO_BUILD_CACHE_IN_CONTAINER}")
  DOCKER_ARGS+=(-v "${DQB_HOST_WORK_DIR}/report:/report")
fi
DOCKER_ARGS+=(-w /work)

# DQB_DOCKER_ENV passes extra KEY=VALUE environment into the container, for example BYDB_ALLOWED_BYTES to size the query pool.
for extra_env in ${DQB_DOCKER_ENV:-}; do
  DOCKER_ARGS+=(-e "${extra_env}")
done

exec docker run "${DOCKER_ARGS[@]}" "${IMAGE}" bash /work/test/integration/distributed/querybench/orchestrate.sh
