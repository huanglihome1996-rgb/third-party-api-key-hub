"""真实 CLI→快照→关闭恢复→显式开放；只证明 fixture 自动演练。"""
import argparse
import datetime as dt
import pathlib
import shutil
import subprocess
import tempfile
import time
from common import CheckFailed, Service, binary_info, binary_paths, emit, query, request, seed, utc_now


def offline(hubd, backup, key, target, succeeds):
    result = subprocess.run([str(hubd), '--fixture', '--restore-from', str(backup),
                             '--fixture-key-file', str(key), '--state-dir', str(target)],
                            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=20, check=False)
    if (result.returncode == 0) != succeeds:
        raise CheckFailed('offline_restore_outcome_mismatch')


def credential_pages(hub, state, revision):
    """Collect the whole inventory only while its business revision is stable."""
    credentials, offset, total = [], 0, None
    while True:
        page = request(hub, state, 'credential.status', {'offset': offset, 'limit': 100})
        entries = page['statuses']
        if (page['revision'] != revision or page['offset'] != offset or page['limit'] != 100
                or not isinstance(page['total'], int) or page['total'] < 0
                or not isinstance(entries, list) or len(entries) > 100):
            raise CheckFailed('credential_inventory_changed')
        if total is None:
            total = page['total']
        if page['total'] != total or offset + len(entries) > total:
            raise CheckFailed('credential_inventory_changed')
        credentials.extend(entries)
        offset += len(entries)
        if offset == total:
            break
        if not entries:
            raise CheckFailed('credential_inventory_incomplete')
    if request(hub, state, 'recovery.status')['revision'] != revision:
        raise CheckFailed('credential_inventory_changed')
    return credentials


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--hub', default='/usr/local/bin/hub')
    parser.add_argument('--hubd', default='/usr/local/bin/hubd')
    parser.add_argument('--output')
    args = parser.parse_args()
    report = {'mode': 'fixture', 'production_rpo_acceptance': 'pending', 'manual_rto_acceptance': 'pending',
              'automated_drill_only': True, 'started_at': utc_now(), 'checks': []}
    try:
        hub, hubd = binary_paths(args)
        report.update(binary_info(hub, hubd))
        with tempfile.TemporaryDirectory(prefix='hub-drill-') as workspace:
            root = pathlib.Path(workspace)
            source, target = root / 'source', root / 'restored'
            with Service(hubd, hub, source):
                seed(hub, source)
                query(hub, source)
                backup_result = request(hub, source, 'operations.backup')
                request(hub, source, 'operations.retention')
                backup_id = backup_result['backup_id']
                if not isinstance(backup_id, str) or pathlib.Path(backup_id).name != backup_id:
                    raise CheckFailed('unsafe_backup_reference')
                backup = source / 'backups' / backup_id
                if any(p.suffix == '.key' for p in backup.rglob('*')):
                    raise CheckFailed('key_in_business_backup')
                report['checks'] += ['backup_created_online', 'retention_cli', 'backup_excludes_key']
            report['failure_at'] = utc_now()
            clock_start = time.perf_counter()
            key = source / 'fixture.key'
            missing = root / 'missing.key'
            offline(hubd, backup, missing, root / 'missing-target', False)
            if missing.exists() or (root / 'missing-target').exists():
                raise CheckFailed('missing_key_or_partial_target_created')
            bad_key = root / 'wrong.key'
            bad_key.write_bytes(b'\x42' * 32)
            bad_key.chmod(0o600)
            offline(hubd, backup, bad_key, root / 'wrong-target', False)
            if (root / 'wrong-target').exists():
                raise CheckFailed('wrong_key_partial_target_created')
            broken = root / 'broken-backup'
            shutil.copytree(backup, broken)
            with (broken / 'business.sqlite').open('ab') as stream:
                stream.write(b'fixture-corruption')
            offline(hubd, broken, key, root / 'broken-target', False)
            if (root / 'broken-target').exists():
                raise CheckFailed('broken_backup_partial_target_created')
            offline(hubd, backup, key, target, True)
            offline(hubd, backup, key, target, False)
            report['checks'] += ['missing_key_rejected', 'wrong_key_rejected', 'corrupted_snapshot_rejected', 'existing_target_rejected']
            for _ in range(2):
                with Service(hubd, hub, target, key):
                    health = request(hub, target, 'recovery.status')
                    if health.get('recovery_closed') is not True:
                        raise CheckFailed('recovery_not_closed')
                    request(hub, target, 'credential.query', {'by': 'external_shop', 'external_shop_id': 'fixture-shop'}, 'credential_disabled')
            report['checks'] += ['restored_query_closed', 'closed_after_restart']
            with Service(hubd, hub, target, key):
                status = request(hub, target, 'recovery.status')
                credentials = credential_pages(hub, target, status['revision'])
                refs = []
                for c in credentials:
                    ref = {key: c[key] for key in ('scope', 'app_id', 'type')}
                    if c['scope'] == 'shop':
                        ref['external_shop_id'] = c['external_shop_id']
                    ref['expected_generation'] = c['generation']
                    refs.append(ref)
                opening = {'expected_revision': status['revision'], 'accounts_verified': True,
                           'ownership_verified': True, 'values_verified': True, 'credentials': refs}
                stale = dict(opening, expected_revision=max(1, status['revision'] - 1))
                request(hub, target, 'recovery.open', stale, 'input_invalid')
                unreviewed = dict(opening, values_verified=False)
                request(hub, target, 'recovery.open', unreviewed, 'input_invalid')
                request(hub, target, 'recovery.open', opening)
                query(hub, target)
                report['opened_at'] = utc_now()
                report['automated_restore_seconds'] = time.perf_counter() - clock_start
                request(hub, target, 'credential.disable', {'scope': 'shop', 'app_id': 'fixture-app', 'external_shop_id': 'fixture-shop', 'type': 'shop_token'})
                request(hub, target, 'credential.query', {'by': 'external_shop', 'external_shop_id': 'fixture-shop'}, 'credential_disabled')
            with Service(hubd, hub, target, key):
                if request(hub, target, 'recovery.status')['recovery_closed']:
                    raise CheckFailed('opened_state_not_persisted')
                request(hub, target, 'credential.query', {'by': 'external_shop', 'external_shop_id': 'fixture-shop'}, 'credential_disabled')
            report['checks'] += ['stale_review_rejected', 'unreviewed_values_rejected', 'explicit_open_complete_result',
                                 'opened_state_persisted', 'disabled_still_rejected_after_restart']
            snapshot_time = dt.datetime.fromisoformat(backup_result['created_at'].replace('Z', '+00:00'))
            failure_time = dt.datetime.fromisoformat(report['failure_at'].replace('Z', '+00:00'))
            report['snapshot_at'] = backup_result['created_at']
            report['fixture_rpo_seconds'] = (failure_time - snapshot_time).total_seconds()
            report['audit_gap'] = backup_result['audit_gap']
            report['status'] = 'PASS' if 0 <= report['fixture_rpo_seconds'] <= 86400 and report['automated_restore_seconds'] <= 14400 else 'FAIL'
    except (CheckFailed, OSError, KeyError, ValueError, subprocess.TimeoutExpired):
        report.update(status='FAIL', failure='fixture_drill_failed')
    report['completed_at'] = utc_now()
    emit(report, args.output)
    return 0 if report['status'] == 'PASS' else 1


if __name__ == '__main__':
    raise SystemExit(main())
