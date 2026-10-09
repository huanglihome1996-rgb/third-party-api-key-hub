"""10 并发完整 CLI fixture 测量；此结果不是生产 KMS 验收。"""
import argparse
import concurrent.futures
import math
import pathlib
import tempfile
import time
from common import CheckFailed, Service, binary_info, binary_paths, emit, query, request, seed, utc_now


def p95(values):
    return sorted(values)[math.ceil(0.95 * len(values)) - 1] if values else None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--hub', default='/usr/local/bin/hub')
    parser.add_argument('--hubd', default='/usr/local/bin/hubd')
    parser.add_argument('--requests', type=int, default=100)
    parser.add_argument('--maintenance', action='store_true')
    parser.add_argument('--fault-after', type=int, help='terminate fixture daemon when this request index starts; expected FAIL')
    parser.add_argument('--output')
    args = parser.parse_args()
    if args.requests < 10 or args.requests > 10000:
        parser.error('requests must be between 10 and 10000')
    if args.fault_after is not None and not 0 <= args.fault_after < args.requests:
        parser.error('fault-after must select a request index')
    started = utc_now()
    durations, successes, failures = [], [], []
    report = {'mode': 'fixture', 'production_kms_acceptance': 'pending', 'concurrency': 10,
              'percentile_method': 'nearest-rank ceil(0.95*n)', 'started_at': started,
              'maintenance': args.maintenance, 'includes_cli_process_and_complete_output': True,
              'warmup_excluded_from_primary_samples': 0, 'errors': {}, 'maintenance_successes': 0}
    report['fault_after'] = args.fault_after
    try:
        hub, hubd = binary_paths(args)
        report.update(binary_info(hub, hubd))
        with tempfile.TemporaryDirectory(prefix='hub-benchmark-') as workspace:
            state = pathlib.Path(workspace) / 'state'
            cold = time.perf_counter()
            with Service(hubd, hub, state) as service:
                report['cold_start_to_health_ms'] = round((time.perf_counter() - cold) * 1000, 3)
                seed(hub, state)
                def one(index):
                    before = time.perf_counter()
                    error = None
                    try:
                        if args.fault_after == index:
                            service.process.terminate()
                        query(hub, state)
                    except CheckFailed as exc:
                        error = str(exc)
                    return (time.perf_counter() - before) * 1000, error
                def maintain():
                    for _ in range(3):
                        request(hub, state, 'operations.backup')
                    return 3
                with concurrent.futures.ThreadPoolExecutor(max_workers=1) as operations_pool, concurrent.futures.ThreadPoolExecutor(max_workers=10) as pool:
                    maintenance = operations_pool.submit(maintain) if args.maintenance else None
                    for latency, error in pool.map(one, range(args.requests)):
                        durations.append(latency)
                        if error:
                            failures.append(latency)
                            report['errors'][error] = report['errors'].get(error, 0) + 1
                        else:
                            successes.append(latency)
                    if maintenance:
                        report['maintenance_successes'] = maintenance.result()
        report.update({'status': 'PASS' if not failures and p95(durations) <= 2000 else 'FAIL',
                       'samples': len(durations), 'success_samples': len(successes), 'failure_samples': len(failures),
                       'all_p95_ms': p95(durations), 'success_p95_ms': p95(successes), 'failure_p95_ms': p95(failures),
                       'max_ms': max(durations), 'latency_ms': [round(x, 3) for x in durations], 'target_ms': 2000})
    except (CheckFailed, OSError):
        report.update(status='FAIL', failure='fixture_execution_failed')
        report.update(samples=len(durations), success_samples=len(successes), failure_samples=len(failures),
                      all_p95_ms=p95(durations), success_p95_ms=p95(successes), failure_p95_ms=p95(failures),
                      latency_ms=[round(x, 3) for x in durations])
    report['completed_at'] = utc_now()
    emit(report, args.output)
    return 0 if report['status'] == 'PASS' else 1


if __name__ == '__main__':
    raise SystemExit(main())
