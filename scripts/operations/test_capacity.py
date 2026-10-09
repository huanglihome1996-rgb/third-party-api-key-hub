"""Capacity evidence must retain failed samples and complete fixture history."""
import json
from contextlib import closing
import pathlib
import sqlite3
import tempfile
import subprocess
import threading
import time
import unittest
from unittest.mock import patch

import capacity
import common


class CapacityEvidenceTests(unittest.TestCase):
    def samples(self):
        return [
            {'index': 0, 'maintenance_operation': 'operations.backup', 'start_ms': 1, 'latency_ms': 10, 'error': None},
            {'index': 0, 'maintenance_operation': 'operations.retention', 'start_ms': 1001, 'latency_ms': 10, 'error': None}]

    def report(self, samples=None):
        return {'samples': self.samples() if samples is None else samples, 'maintenance': [
            {'operation': 'operations.backup', 'start_ms': 0, 'latency_ms': 1000, 'error': None},
            {'operation': 'operations.retention', 'start_ms': 1000, 'latency_ms': 1000, 'error': None}]}

    def test_slow_failed_sample_remains_in_primary_percentile(self):
        samples = self.samples()
        samples.append({'index': 1, 'maintenance_operation': 'operations.backup', 'start_ms': 1, 'latency_ms': 10000, 'error': 'cli_timeout'})
        report = self.report(samples)
        capacity.summarize(report, 1)
        self.assertEqual(report['status'], 'FAIL')
        self.assertEqual(report['all_p95_ms'], 10000)
        self.assertEqual(report['success_p95_ms'], 10)
        self.assertEqual(report['failure_p95_ms'], 10000)
        self.assertEqual((report['success_samples'], report['failure_samples']), (2, 1))
        self.assertEqual(report['samples'], samples)

    def test_partial_or_late_failed_run_preserves_completed_samples(self):
        for late_failure in (False, True):
            samples = self.samples() if late_failure else self.samples()[:1]
            report = self.report(samples)
            if late_failure:
                report['failure'] = 'daemon_shutdown_failed'
            capacity.summarize(report, 1)
            self.assertEqual(report['status'], 'FAIL')
            self.assertEqual(report['success_samples'], len(samples))
            self.assertEqual(report['all_p95_ms'], 10)
            self.assertEqual(report['samples'], samples)

    def test_gate_requires_successful_queries_overlapping_each_operation(self):
        report = self.report()
        capacity.summarize(report, 1)
        self.assertEqual(report['status'], 'PASS')
        self.assertEqual(report['queries_overlapping_by_operation'], {'operations.backup': 1, 'operations.retention': 1})
        for defect in ('query_budget', 'maintenance_error', 'maintenance_budget', 'no_retention_overlap'):
            with self.subTest(defect=defect):
                report = self.report()
                if defect == 'query_budget':
                    report['samples'][0]['latency_ms'] = 2001
                elif defect == 'maintenance_error':
                    report['maintenance'][1]['error'] = 'cli_timeout'
                elif defect == 'maintenance_budget':
                    report['maintenance'][1]['latency_ms'] = 15001
                else:
                    report['samples'][1]['start_ms'] = 2001
                capacity.summarize(report, 1)
                self.assertEqual(report['status'], 'FAIL')

    def test_execution_keeps_completed_samples_before_late_worker_exception(self):
        report = {'samples': [], 'maintenance': []}
        lock = threading.Lock()
        calls = [0]
        observed = []
        def late_query(*_):
            with lock:
                calls[0] += 1
                number = calls[0]
            if number == 10:
                deadline = time.monotonic() + 1
                while not report['samples'] and time.monotonic() < deadline:
                    time.sleep(0.001)
                observed.append(bool(report['samples']))
                raise RuntimeError('untrusted worker error text')
        def maintenance(*_):
            time.sleep(0.01)
        with patch.object(capacity, 'query', side_effect=late_query), patch.object(capacity, 'request', side_effect=maintenance):
            origin = time.perf_counter()
            for operation in ('operations.backup', 'operations.retention'):
                capacity.measure_manual_operation(report, None, None, operation, 10, origin)
        capacity.summarize(report, 10)
        self.assertEqual(observed, [True])
        self.assertEqual(len(report['samples']), 20)
        self.assertEqual(len(report['maintenance']), 2)
        self.assertEqual(report['failure_samples'], 1)
        self.assertEqual(report['status'], 'FAIL')
        failed = [sample for sample in report['samples'] if sample['error']]
        self.assertEqual(failed[0]['error'], 'query_worker_failed')
        self.assertGreaterEqual(failed[0]['latency_ms'], 0)

    def test_request_timeout_keeps_fixed_category_and_supervision_deadline(self):
        for operation in ('credential.query', 'operations.backup', 'operations.retention'):
            with patch.object(common.subprocess, 'run', side_effect=subprocess.TimeoutExpired('hub', 20)) as run:
                with self.assertRaisesRegex(common.CheckFailed, '^cli_timeout$'):
                    common.request(None, pathlib.Path('fixture-state'), operation)
            self.assertEqual(run.call_args.kwargs['timeout'], 20)

    def test_execution_records_actual_elapsed_time_for_timeout(self):
        report = {'samples': [], 'maintenance': []}
        def timeout(*_, **__):
            time.sleep(0.003)
            raise subprocess.TimeoutExpired('fixture-hub', 20)
        with patch.object(common.subprocess, 'run', side_effect=timeout), patch.object(capacity, 'query'):
            origin = time.perf_counter()
            for operation in ('operations.backup', 'operations.retention'):
                capacity.measure_manual_operation(report, pathlib.Path('fixture-hub'), pathlib.Path('fixture-state'), operation, 10, origin)
        capacity.summarize(report, 10)
        self.assertEqual(report['status'], 'FAIL')
        self.assertEqual(len(report['samples']), 20)
        for outcome in report['maintenance']:
            self.assertEqual(outcome['error'], 'cli_timeout')
            self.assertGreaterEqual(outcome['latency_ms'], 2)

    def test_main_emits_failure_if_environment_read_fails(self):
        with patch.object(capacity.pathlib.Path, 'is_file', return_value=True), \
             patch.object(capacity.pathlib.Path, 'read_text', side_effect=PermissionError('private diagnostic')), \
             patch('sys.argv', ['capacity.py', '--events', '21', '--packages', '3', '--requests', '10']), \
             patch.object(capacity, 'emit') as emit:
            self.assertEqual(capacity.main(), 1)
        report = emit.call_args.args[0]
        self.assertEqual(report['status'], 'FAIL')
        self.assertEqual(report['failure'], 'capacity_execution_failed')
        self.assertEqual(report['samples'], [])
        self.assertNotIn('private diagnostic', json.dumps(report))

    def test_main_worker_failure_keeps_both_completed_batches(self):
        calls = [0]
        lock = threading.Lock()
        def late_query(*_):
            with lock:
                calls[0] += 1
                number = calls[0]
            if number == 10:
                time.sleep(0.005)
                raise RuntimeError('private worker diagnostic')
        def request(_hub, _state, operation, _payload=None):
            if operation == 'audit.list':
                return {'total': 21, 'events': [{}]}
            time.sleep(0.01)
            return {}
        with patch.object(capacity.pathlib.Path, 'is_file', return_value=False), \
             patch.object(capacity, 'binary_paths', return_value=('fixture-hub', 'fixture-hubd')), \
             patch.object(capacity, 'binary_info', return_value={}), \
             patch.object(capacity, 'prepare_history'), patch.object(capacity, 'Service'), \
             patch.object(capacity, 'query', side_effect=late_query), \
             patch.object(capacity, 'request', side_effect=request), \
             patch('sys.argv', ['capacity.py', '--events', '21', '--packages', '3', '--requests', '10']), \
             patch.object(capacity, 'emit') as emit:
            self.assertEqual(capacity.main(), 1)
        report = emit.call_args.args[0]
        self.assertEqual(len(report['samples']), 20)
        self.assertEqual(len(report['maintenance']), 2)
        self.assertEqual(report['failure_samples'], 1)
        self.assertEqual(report['status'], 'FAIL')
        self.assertFalse(report['automatic_maintenance_verified'])
        self.assertEqual(report['automatic_schedule_evidence'], 'separate_go_capacity_report_required')
        self.assertNotIn('private worker diagnostic', json.dumps(report))

    def test_copied_packages_keep_every_synthetic_event_and_file(self):
        with tempfile.TemporaryDirectory() as workspace:
            state = pathlib.Path(workspace)
            with closing(sqlite3.connect(state / 'audit.sqlite')) as db, db:
                db.execute('CREATE TABLE events(id INTEGER PRIMARY KEY AUTOINCREMENT, record TEXT NOT NULL)')
            capacity.seed_history(state, 21)
            source = state / 'backups' / 'backup-source'
            source.mkdir(parents=True)
            (source / 'business.sqlite').write_bytes(b'fixture-business')
            (source / 'audit.sqlite').write_bytes((state / 'audit.sqlite').read_bytes())
            manifest = {'created_at': '2026-01-01T00:00:00Z', 'files': {'business.sqlite': 'fixture-digest', 'audit.sqlite': 'fixture-digest'}}
            (source / 'manifest.json').write_text(json.dumps(manifest))
            measured = capacity.copy_history(state, source, 3)
            packages = list((state / 'backups').iterdir())
            self.assertEqual(len(packages), 3)
            self.assertEqual(measured, sum(path.stat().st_size for package in packages for path in package.iterdir()))
            for package in packages:
                self.assertEqual({path.name for path in package.iterdir()}, {'business.sqlite', 'audit.sqlite', 'manifest.json'})
                self.assertEqual((package / 'business.sqlite').read_bytes(), b'fixture-business')
                with closing(sqlite3.connect(package / 'audit.sqlite')) as db:
                    events = [json.loads(row[0]) for row in db.execute('SELECT record FROM events ORDER BY id')]
                self.assertEqual(len(events), 21)
                self.assertEqual(len({event['operation_id'] for event in events}), 21)
                for event in events:
                    self.assertGreaterEqual(event['time'], (capacity.dt.datetime.now(capacity.dt.timezone.utc) - capacity.dt.timedelta(days=366)).isoformat().replace('+00:00', 'Z'))
                self.assertEqual(json.loads((package / 'manifest.json').read_text())['files'], manifest['files'])


if __name__ == '__main__':
    unittest.main()
