"""隔离 fixture 的 CLI 演练支持；不保存或显示 CLI 秘密响应。"""
import datetime as dt
import hashlib
import json
import os
import pathlib
import platform
import subprocess
import time


class CheckFailed(Exception):
    pass


STABLE_ERRORS = {'input_invalid', 'not_found', 'selection_required', 'mapping_incomplete',
                 'mapping_mismatch', 'permission_denied', 'credential_missing',
                 'credential_expired', 'credential_disabled', 'internal_error'}


def utc_now():
    return dt.datetime.now(dt.timezone.utc).isoformat().replace('+00:00', 'Z')


def request(hub, state, operation, payload=None, expected_error=None):
    wire = json.dumps({'version': 'contract/v1', 'operation': operation, 'payload': payload or {}}).encode()
    try:
        result = subprocess.run([str(hub), '--socket', str(pathlib.Path(state) / 'hub.sock')],
                                input=wire, capture_output=True, timeout=20, check=False)
        data = json.loads(result.stdout)
    except subprocess.TimeoutExpired:
        raise CheckFailed('cli_timeout') from None
    except (OSError, ValueError):
        raise CheckFailed('cli_execution_failed') from None
    if not isinstance(data, dict):
        raise CheckFailed('invalid_cli_response')
    if expected_error is not None:
        if result.returncode != 1 or data.get('error') != expected_error or set(data) != {'error', 'message'}:
            raise CheckFailed('expected_rejection_missing')
        return data
    if result.returncode or 'error' in data:
        if result.returncode == 1 and set(data) == {'error', 'message'} and data.get('error') in STABLE_ERRORS:
            raise CheckFailed(data['error'])
        raise CheckFailed('cli_operation_failed')
    return data


class Service:
    def __init__(self, hubd, hub, state, key=None):
        self.hubd, self.hub, self.state, self.key = hubd, hub, pathlib.Path(state), key
        self.process = None

    def start(self):
        command = [str(self.hubd), '--fixture', '--state-dir', str(self.state)]
        if self.key:
            command += ['--fixture-key-file', str(self.key)]
        self.process = subprocess.Popen(command, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            if self.process.poll() is not None:
                raise CheckFailed('daemon_start_failed')
            try:
                request(self.hub, self.state, 'health')
                return
            except CheckFailed:
                time.sleep(0.02)
        self.stop()
        raise CheckFailed('daemon_start_timeout')

    def stop(self):
        if self.process and self.process.poll() is None:
            self.process.terminate()
            try:
                self.process.wait(timeout=20)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait(timeout=5)
                raise CheckFailed('daemon_shutdown_timeout') from None
            if self.process.returncode != 0:
                raise CheckFailed('daemon_shutdown_failed')

    def __enter__(self):
        try:
            self.start()
        except BaseException:
            self.stop()
            raise
        return self

    def __exit__(self, *_):
        self.stop()


def seed(hub, state):
    cases = [
        ('app.register', {'app_id': 'fixture-app', 'platform': 'fixture', 'provider_app_id': 'fixture-provider'}),
        ('shop.register', {'external_shop_id': 'fixture-shop', 'platform': 'fixture', 'provider_shop_id': 'fixture-provider-shop'}),
        ('brand.register', {'brand_id': 'fixture-brand'}),
        ('internal_shop.register', {'internal_shop_id': 'fixture-internal'}),
        ('shop.map_brand', {'external_shop_id': 'fixture-shop', 'brand_id': 'fixture-brand', 'expected_revision': 1}),
        ('brand.map_internal_shop', {'brand_id': 'fixture-brand', 'internal_shop_id': 'fixture-internal', 'expected_revision': 1}),
    ]
    for op, data in cases:
        request(hub, state, op, data)
    for scope, typ, value in [('app', 'client_secret', 'fixture-benchmark-app'), ('shop', 'shop_token', 'fixture-benchmark-shop')]:
        payload = {'scope': scope, 'app_id': 'fixture-app', 'type': typ, 'value': value,
                   'replace': False, 'expiry_known': False, 'expires_at': None, 'confirm': True}
        if scope == 'shop':
            payload['external_shop_id'] = 'fixture-shop'
        request(hub, state, 'credential.import', payload)


def query(hub, state):
    data = request(hub, state, 'credential.query', {'by': 'external_shop', 'external_shop_id': 'fixture-shop'})
    expected = {'app': [{'type': 'client_secret', 'value': 'fixture-benchmark-app'}],
                'shop': [{'type': 'shop_token', 'value': 'fixture-benchmark-shop'}]}
    if data.get('credentials') != expected or data.get('identity', {}).get('brand_id') != 'fixture-brand':
        raise CheckFailed('fixture_result_mismatch')
    for scope in ('app', 'shop'):
        entries = data.get('status', {}).get(scope, [])
        if len(entries) != 1 or entries[0].get('generation') != 1 or not entries[0].get('available'):
            raise CheckFailed('fixture_status_mismatch')


def binary_paths(args):
    if os.name != 'posix':
        raise CheckFailed('linux_container_required')
    hub, hubd = pathlib.Path(args.hub).resolve(), pathlib.Path(args.hubd).resolve()
    if not hub.is_file() or not hubd.is_file():
        raise CheckFailed('binaries_missing')
    return hub, hubd


def binary_info(hub, hubd):
    hashes, architectures = {}, {}
    for name, path in [('hub', hub), ('hubd', hubd)]:
        with path.open('rb') as stream:
            hashes[name] = hashlib.file_digest(stream, 'sha256').hexdigest()
            stream.seek(0)
            header = stream.read(20)
        machine = int.from_bytes(header[18:20], 'little') if header[:4] == b'\x7fELF' else 0
        architectures[name] = {62: 'amd64', 183: 'arm64'}.get(machine, 'unknown')
    return {'binary_sha256': hashes, 'binary_architectures': architectures,
            'runtime_machine': platform.machine(), 'uid': os.getuid()}


def emit(report, output=None):
    wire = json.dumps(report, ensure_ascii=False, indent=2) + '\n'
    if output:
        target = pathlib.Path(output)
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(wire, encoding='utf-8')
    print(wire, end='')
