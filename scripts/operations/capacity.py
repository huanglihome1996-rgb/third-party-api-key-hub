"""Historical fixture capacity: complete CLI queries while backup/retention run."""
import argparse
import concurrent.futures
from contextlib import closing
import datetime as dt
import json
import os
import pathlib
import shutil
import sqlite3
import tempfile
import threading
import time

from benchmark import p95
from common import CheckFailed, Service, binary_info, binary_paths, emit, query, request, seed, utc_now


def seed_history(state, count):
    """Only called with the isolated daemon stopped; no real state is accepted."""
    now = dt.datetime.now(dt.timezone.utc)
    def records():
        for index in range(count):
            # Every synthetic record is safely inside the retained year. Daily
            # maintenance remains enabled and cannot race away this workload.
            # The separate Go capacity measurement exercises expired deletion.
            at = now - dt.timedelta(days=index % 365)
            yield (json.dumps({'time': at.isoformat().replace('+00:00', 'Z'), 'uid': 10001,
                    'actor_name': 'fixture-capacity', 'operation': 'app.list',
                    'target': 'app_id:fixture-app', 'result': 'success',
                    'operation_id': 'op-capacity-' + str(index)}),)
    with closing(sqlite3.connect(state / 'audit.sqlite')) as db, db:
        db.executemany('INSERT INTO events(record) VALUES(?)', records())


def copy_history(state, source, count):
    now = dt.datetime.now(dt.timezone.utc)
    for index in range(1, count):
        target = state / 'backups' / ('backup-capacity-' + str(index))
        shutil.copytree(source, target)
        manifest_path = target / 'manifest.json'
        manifest = json.loads(manifest_path.read_text())
        manifest['created_at'] = (now - dt.timedelta(days=index)).isoformat().replace('+00:00', 'Z')
        manifest_path.write_text(json.dumps(manifest), encoding='utf-8')
    return sum(path.stat().st_size for path in (state / 'backups').glob('*/*') if path.is_file())


def summarize(report, requested):
    """Requested is the query count per manual operation, not the total."""
    samples = report['samples']
    success = [sample['latency_ms'] for sample in samples if sample['error'] is None]
    failure = [sample['latency_ms'] for sample in samples if sample['error'] is not None]
    all_values = [sample['latency_ms'] for sample in samples]
    by_operation = {}
    overlaps = {}
    for operation in ('operations.backup', 'operations.retention'):
        batch = [sample for sample in samples if sample.get('maintenance_operation') == operation]
        by_operation[operation] = {'samples': len(batch), 'all_p95_ms': p95([sample['latency_ms'] for sample in batch])}
        overlaps[operation] = sum(any(item['operation'] == operation and
            sample['start_ms'] < item['start_ms'] + item['latency_ms'] and
            sample['start_ms'] + sample['latency_ms'] > item['start_ms']
            for item in report['maintenance']) for sample in batch)
    report.update(success_samples=len(success), failure_samples=len(failure),
                  all_p95_ms=p95(all_values), success_p95_ms=p95(success), failure_p95_ms=p95(failure),
                  max_ms=max(all_values) if all_values else None, queries_overlapping_by_operation=overlaps,
                  query_batches=by_operation)
    report['status'] = 'PASS' if 'failure' not in report and len(samples) == 2 * requested and not failure and \
        all(batch['samples'] == requested and batch['all_p95_ms'] <= 2000 for batch in by_operation.values()) and \
        len(report['maintenance']) == 2 and all(overlap > 0 for overlap in overlaps.values()) and all(
        item['error'] is None and item['latency_ms'] <= 15000 for item in report['maintenance']) else 'FAIL'


def measure_manual_operation(report, hub, state, operation, requested, origin):
    """Collect each completed outcome before a later worker can fail."""
    started = threading.Event()
    def one(index):
        started.wait()
        before = time.perf_counter()
        error = None
        try:
            query(hub, state)
        except CheckFailed as exc:
            error = str(exc)
        except Exception:
            error = 'query_worker_failed'
        return {'index': index, 'maintenance_operation': operation,
                'start_ms': round((before - origin) * 1000, 3),
                'latency_ms': round((time.perf_counter() - before) * 1000, 3), 'error': error}
    def maintain():
        before = time.perf_counter()
        started.set()
        error = None
        try:
            request(hub, state, operation)
        except CheckFailed as exc:
            error = str(exc)
        except Exception:
            error = 'maintenance_worker_failed'
        return {'operation': operation, 'start_ms': round((before - origin) * 1000, 3),
                'latency_ms': round((time.perf_counter() - before) * 1000, 3), 'error': error}
    with concurrent.futures.ThreadPoolExecutor(max_workers=1) as operations, concurrent.futures.ThreadPoolExecutor(max_workers=10) as queries:
        futures = {operations.submit(maintain): 'maintenance'}
        futures.update({queries.submit(one, index): 'samples' for index in range(requested)})
        for future in concurrent.futures.as_completed(futures):
            # Workers normalize their failures, so all attempted outcomes are
            # retained. Append at completion rather than assigning a final list.
            report[futures[future]].append(future.result())


def count_history(path):
    with closing(sqlite3.connect(path)) as db:
        return db.execute("SELECT COUNT(*) FROM events WHERE instr(record, 'op-capacity-') > 0").fetchone()[0]


def prepare_history(hub, hubd, state, events, packages, report):
    """Offline changes only affect a new temporary fixture, while stopped."""
    with Service(hubd, hub, state):
        seed(hub, state)
    seed_history(state, events)
    with Service(hubd, hub, state):
        backup = request(hub, state, 'operations.backup')
    source = state / 'backups' / backup['backup_id']
    if count_history(source / 'audit.sqlite') != events or count_history(state / 'audit.sqlite') != events:
        raise CheckFailed('snapshot_history_incomplete')
    for other in (state / 'backups').iterdir():
        if other != source:
            shutil.rmtree(other)
    report['synthetic_history_retention'] = 'all_within_365_days'
    report['historical_backup_bytes'] = copy_history(state, source, packages)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--hub', default='/usr/local/bin/hub')
    parser.add_argument('--hubd', default='/usr/local/bin/hubd')
    parser.add_argument('--events', type=int, default=100000)
    parser.add_argument('--packages', type=int, default=100)
    parser.add_argument('--requests', type=int, default=100, help='complete queries per manual operation (two batches)')
    parser.add_argument('--output')
    args = parser.parse_args()
    if not 1 <= args.events <= 1000000 or not 1 <= args.packages <= 366 or not 10 <= args.requests <= 10000:
        parser.error('events 1..1000000, packages 1..366, requests 10..10000 required')
    report = {'mode': 'fixture', 'production_capacity_acceptance': 'pending', 'started_at': utc_now(),
              'synthetic_history_events': args.events, 'historical_packages': args.packages,
              'concurrency': 10, 'includes_cli_process_and_complete_output': True,
              'percentile_method': 'nearest-rank ceil(0.95*n)', 'query_target_ms': 2000,
              'manual_maintenance_target_ms': 15000, 'subprocess_supervision_timeout_ms': 20000,
              'automatic_maintenance_verified': False, 'automatic_schedule_evidence': 'separate_go_capacity_report_required',
              'requested_queries_per_operation': args.requests, 'samples': [], 'maintenance': []}
    try:
        report['runtime_cpu_count'] = os.cpu_count()
        for field, path in (('cgroup_cpu_max', '/sys/fs/cgroup/cpu.max'), ('cgroup_memory_max', '/sys/fs/cgroup/memory.max')):
            if pathlib.Path(path).is_file():
                report[field] = pathlib.Path(path).read_text().strip()
        for field, path, prefix in (('cpu_affinity', '/proc/self/status', 'Cpus_allowed_list:'),
                                    ('host_memory_total', '/proc/meminfo', 'MemTotal:')):
            if pathlib.Path(path).is_file():
                for line in pathlib.Path(path).read_text().splitlines():
                    if line.startswith(prefix):
                        report[field] = line[len(prefix):].strip()
        kernel = pathlib.Path('/proc/sys/kernel/osrelease')
        if kernel.is_file():
            report['kernel_release'] = kernel.read_text().strip()
        hub, hubd = binary_paths(args)
        report.update(binary_info(hub, hubd))
        with tempfile.TemporaryDirectory(prefix='hub-capacity-') as workspace:
            state = pathlib.Path(workspace) / 'state'
            prepare_history(hub, hubd, state, args.events, args.packages, report)
            restart = time.perf_counter()
            with Service(hubd, hub, state):
                report['historical_restart_to_health_ms'] = round((time.perf_counter() - restart) * 1000, 3)
                start = time.perf_counter()
                try:
                    audit = request(hub, state, 'audit.list', {'limit': 1})
                except CheckFailed as exc:
                    report['historical_audit_list_error'] = str(exc)
                    raise
                finally:
                    report['historical_audit_list_cli_ms'] = round((time.perf_counter() - start) * 1000, 3)
                report['historical_audit_events_total'] = audit.get('total')
                if not isinstance(audit.get('total'), int) or audit['total'] < args.events or len(audit.get('events', [])) != 1:
                    raise CheckFailed('audit_history_incomplete')
                origin = time.perf_counter()
                for operation in ('operations.backup', 'operations.retention'):
                    measure_manual_operation(report, hub, state, operation, args.requests, origin)
    except Exception as exc:
        report['failure'] = str(exc) if isinstance(exc, CheckFailed) else 'capacity_execution_failed'
    summarize(report, args.requests)
    report['completed_at'] = utc_now()
    emit(report, args.output)
    return 0 if report['status'] == 'PASS' else 1


if __name__ == '__main__':
    raise SystemExit(main())
