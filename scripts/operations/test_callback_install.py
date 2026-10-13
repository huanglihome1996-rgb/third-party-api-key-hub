"""HTTP installer regression tests: real files, mocked host-service boundaries."""
import importlib.util
import io
import json
import os
import shutil
import socket
import subprocess
from pathlib import Path
import struct
import sys
import tempfile
import threading
import unittest
from unittest import mock
from http.server import BaseHTTPRequestHandler, HTTPServer

MODULE = Path(os.environ.get("CALLBACK_INSTALL_MODULE", Path(__file__).resolve().parents[2] / "deploy/tiktok-callback/install.py"))
spec = importlib.util.spec_from_file_location("callback_install", MODULE)
installer = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = installer
spec.loader.exec_module(installer)


class FakeSystem:
    def __init__(self, owner):
        self.owner = owner
        self.commands = []
        self.active = self.enabled = self.fail_health = self.fail_restart = False
        self.blocked_ports = set()
        self.health_addresses = []
        self.live_callback = None
        self.stop_fails = False
        self.runtime_enabled = False
        self.group_exists = True
        self.named_gid = os.getgid()

    def run(self, args, check=True):
        self.commands.append(args)
        if args[:2] == ["id", "-u"]:
            return str(os.getuid())
        if args[:2] == ["id", "-g"]:
            return str(os.getgid())
        if args[:2] == ["getent", "group"]:
            return "api-key-hub:x:" + str(self.named_gid) + ":" if self.group_exists else ""
        if args[0] == "groupadd":
            self.group_exists = True
        if args[:2] == ["systemctl", "is-active"]:
            return "active" if self.active else "inactive"
        if args[:2] == ["systemctl", "is-enabled"]:
            return "enabled-runtime" if self.runtime_enabled else ("enabled" if self.enabled else "disabled")
        if args[:2] == ["systemctl", "restart"]:
            if self.fail_restart:
                self.fail_restart = False
                raise installer.InstallError("service failed")
            self.active = True
        if args[:2] == ["systemctl", "stop"]:
            if not self.stop_fails:
                self.active = False
        if args[:2] == ["systemctl", "enable"]:
            self.runtime_enabled = "--runtime" in args
            self.enabled = not self.runtime_enabled
        if args[:2] == ["systemctl", "disable"]:
            if not self.owner.p(installer.UNIT).exists() and check:
                raise installer.InstallError("unit does not exist")
            self.enabled = False
            self.runtime_enabled = False
        return ""

    def which(self, name):
        return "/usr/bin/" + name

    def port_available(self, host, port):
        return port not in self.blocked_ports

    def service_owns_port(self, port):
        return self.active and port == 9080

    def health(self, host, port):
        self.health_addresses.append((host, port))
        if self.live_callback:
            self.live_callback()
        if self.fail_health:
            raise installer.InstallError("HTTP health failed")


class InstallTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.root.chmod(0o755)
        (self.root / "opt").mkdir(mode=0o755)
        (self.root / "etc").mkdir()
        (self.root / "etc/os-release").write_text('ID=ubuntu\nVERSION_ID="24.04"\n')
        (self.root / "run/systemd/system").mkdir(parents=True)
        self.binary = self.root / "source/ttcallback"
        self.binary.parent.mkdir()
        machine = 183 if installer.platform.machine() == "aarch64" else 62
        self.binary.write_bytes(b"\x7fELF\x02\x01" + bytes(12) + struct.pack("<H", machine) + bytes(44))
        self.system = FakeSystem(self)
        self.subject = installer.Installer(root=self.root, system=self.system)

    def p(self, path):
        return self.root / path.lstrip("/")

    def install(self, apps=None, domain="callback.example.com", **kwargs):
        return self.subject.install(domain, apps or [{"id": "main", "key": "key-main", "secret": "secret-main"}], binary=self.binary, **kwargs)

    def config(self):
        return json.loads(self.p(installer.CONFIG).read_text())

    def test_first_install_starts_http_and_keeps_public_https_callback(self):
        self.install()
        cfg = self.config()
        self.assertEqual(set(cfg["apps"]), {"main"})
        self.assertEqual(cfg["listen_addr"], "0.0.0.0:9080")
        app = cfg["apps"]["main"]
        self.assertNotIn("service_id", app.get("authorize_params", {}))
        self.assertEqual(app["redirect_url"], "https://callback.example.com/callbacks/tiktok-shop")
        secret = self.p(app["app_secret_file"])
        self.assertEqual(secret.read_text(), "secret-main\n")
        self.assertEqual(secret.stat().st_mode & 0o777, 0o600)
        self.assertEqual(len(self.p(installer.STORE_KEY).read_bytes()), 32)
        self.assertNotIn("secret-main", json.dumps(self.system.commands))
        self.assertIn(" -http", self.p(installer.UNIT).read_text())
        self.assertNotIn("tls-cert", self.p(installer.UNIT).read_text())
        self.assertFalse(self.p("/etc/letsencrypt").exists())
        self.assertFalse(self.p("/etc/api-key-hub/tls").exists())
        self.assertEqual(self.system.health_addresses, [("127.0.0.1", 9080)])
        self.assertTrue(self.system.active)
        self.assertTrue(self.system.enabled)

    def test_packaged_systemd_unit_matches_installer_template(self):
        packaged = MODULE.with_name(installer.SERVICE)
        if packaged.exists():
            self.assertEqual(packaged.read_text().replace("\r\n", "\n").strip(), installer.DEFAULT_UNIT.strip())

    def test_shell_bootstraps_missing_ss_without_executing_real_apt(self):
        executable_dir = self.root / "fake-bin"
        executable_dir.mkdir()
        log = self.root / "boundary.log"
        for name in ("apt-get", "python3"):
            executable = executable_dir / name
            executable.write_text('#!/bin/bash\nprintf "%s\\n" "$*" >> "$INSTALLER_BOUNDARY_LOG"\n')
            executable.chmod(0o755)
        (executable_dir / "dirname").symlink_to(shutil.which("dirname"))
        script = MODULE.with_name("install.sh").read_text().replace("/etc/os-release", str(self.root / "etc/os-release")).replace("/run/systemd/system", str(self.root / "run/systemd/system"))
        entry = self.root / "bootstrap.sh"
        entry.write_text(script)
        environment = dict(os.environ, PATH=str(executable_dir), INSTALLER_BOUNDARY_LOG=str(log))
        result = subprocess.run([shutil.which("bash"), str(entry)], env=environment, capture_output=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        lines = log.read_text().splitlines()
        self.assertEqual(lines[:2], ["update", "install -y iproute2"])
        self.assertNotIn("systemctl", log.read_text())

    def test_umask_077_still_allows_nonroot_to_read_and_execute_installed_binary(self):
        previous = os.umask(0o077)
        try:
            self.install()
        finally:
            os.umask(previous)

        def as_nonroot():
            os.setgroups([])
            os.setgid(65534)
            os.setuid(65534)

        probe = "import os,sys; p=sys.argv[1]; open(p,'rb').read(4); assert os.access(p,os.X_OK)"
        result = subprocess.run([sys.executable, "-c", probe, str(self.p(installer.BINARY))], preexec_fn=as_nonroot, capture_output=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        for directory in ("/opt/api-key-hub", "/opt/api-key-hub/bin"):
            info = self.p(directory).stat()
            self.assertEqual(info.st_mode & 0o777, 0o755)
            self.assertEqual((info.st_uid, info.st_gid), (0, 0))

    def test_existing_user_missing_named_group_is_created_before_install_writes(self):
        self.system.group_exists = False
        self.system.named_gid = 2001
        self.install()
        self.assertTrue(self.system.group_exists)
        self.assertIn(["groupadd", "--system", "api-key-hub"], self.system.commands)
        self.assertEqual(self.p(installer.CONFIG).stat().st_gid, 2001)
        self.assertEqual(self.p("/etc/api-key-hub").stat().st_gid, 2001)

    def test_custom_listen_addr_and_invalid_port(self):
        self.install(listen_addr="127.0.0.1:19080")
        self.assertEqual(self.config()["listen_addr"], "127.0.0.1:19080")
        self.assertEqual(self.system.health_addresses[-1], ("127.0.0.1", 19080))
        self.install(listen_addr="192.0.2.4:19080")
        self.assertEqual(self.system.health_addresses[-1], ("192.0.2.4", 19080))
        for value in ("0.0.0.0:80", "0.0.0.0:65536", "example.com:9080", "0.0.0.0:9080;bad"):
            with self.subTest(value=value), self.assertRaises(installer.InstallError):
                self.install(listen_addr=value)

    def test_rerun_preserves_other_apps_key_tokens_states_and_updates_all_redirects(self):
        self.install([{"id": "main", "key": "key1", "secret": "old"}, {"id": "second", "key": "key2", "secret": "keep"}])
        original_key = self.p(installer.STORE_KEY).read_bytes()
        self.p(installer.TOKEN_STORE).write_bytes(b"encrypted tokens")
        self.p(installer.STATE_STORE).write_bytes(b"states")
        self.install(domain="new.example.com")
        cfg = self.config()
        self.assertEqual(set(cfg["apps"]), {"main", "second"})
        self.assertEqual(self.p("/etc/api-key-hub/secrets/second.secret").read_text(), "keep\n")
        self.assertEqual(self.p(installer.STORE_KEY).read_bytes(), original_key)
        self.assertEqual(self.p(installer.TOKEN_STORE).read_bytes(), b"encrypted tokens")
        self.assertEqual(self.p(installer.STATE_STORE).read_bytes(), b"states")
        for app in cfg["apps"].values():
            self.assertEqual(app["redirect_url"], "https://new.example.com/callbacks/tiktok-shop")

    def test_http_health_failure_restores_original_files_directory_mode_and_running_state(self):
        self.install()
        directory = self.p("/var/lib/api-key-hub")
        directory.chmod(0o750)
        paths = [installer.CONFIG, installer.BINARY, installer.UNIT, installer.STORE_KEY, "/etc/api-key-hub/secrets/main.secret"]
        before = {p: (self.p(p).read_bytes(), self.p(p).stat().st_mode & 0o777) for p in paths}
        self.system.fail_health = True
        self.binary.write_bytes(self.binary.read_bytes() + b"new")
        with self.assertRaises(installer.InstallError):
            self.install(domain="new.example.com")
        for path, (content, mode) in before.items():
            self.assertEqual(self.p(path).read_bytes(), content, path)
            self.assertEqual(self.p(path).stat().st_mode & 0o777, mode, path)
        self.assertEqual(directory.stat().st_mode & 0o777, 0o750)
        self.assertTrue(self.system.active)

    def test_first_failure_without_live_data_removes_new_sensitive_files_and_stops_service(self):
        self.system.fail_health = True
        with self.assertRaises(installer.InstallError):
            self.install()
        for path in (installer.CONFIG, installer.STORE_KEY, "/etc/api-key-hub/secrets/main.secret"):
            self.assertFalse(self.p(path).exists(), path)
        self.assertFalse(self.system.active)
        self.assertFalse(self.system.enabled)

    def test_first_failure_with_live_token_preserves_matching_key_config_and_secret(self):
        self.system.fail_health = True
        generated = []

        def new_token():
            generated.append(self.p(installer.STORE_KEY).read_bytes())
            self.p(installer.TOKEN_STORE).write_bytes(b"new encrypted token")

        self.system.live_callback = new_token
        with self.assertRaises(installer.InstallError) as failure:
            self.install()
        self.assertIn("数据", str(failure.exception))
        self.assertEqual(self.p(installer.STORE_KEY).read_bytes(), generated[0])
        self.assertEqual(self.p(installer.TOKEN_STORE).read_bytes(), b"new encrypted token")
        self.assertEqual(self.config()["apps"]["main"]["app_key"], "key-main")
        self.assertTrue(self.p("/etc/api-key-hub/secrets/main.secret").exists())
        self.assertFalse(self.system.active)

    def recovery(self):
        directories = list(self.p(installer.RECOVERY_ROOT).iterdir())
        self.assertEqual(len(directories), 1)
        recovery = directories[0]
        self.assertEqual(recovery.stat().st_mode & 0o777, 0o700)
        self.assertEqual(recovery.stat().st_uid, 0)
        return recovery

    def test_existing_config_new_key_live_token_preserves_key_and_recovery_snapshot(self):
        self.install()
        before = self.p(installer.CONFIG).read_bytes()
        self.p(installer.STORE_KEY).unlink()
        generated = []

        def new_token():
            generated.append(self.p(installer.STORE_KEY).read_bytes())
            self.p(installer.TOKEN_STORE).write_bytes(b"new token")

        self.system.live_callback, self.system.fail_health = new_token, True
        with self.assertRaises(installer.InstallError) as failure:
            self.install(domain="new.example.com")
        self.assertEqual(self.p(installer.CONFIG).read_bytes(), before)
        self.assertEqual(self.p(installer.STORE_KEY).read_bytes(), generated[0])
        self.assertEqual(self.p(installer.TOKEN_STORE).read_bytes(), b"new token")
        recovery = self.recovery()
        self.assertIn(str(recovery), str(failure.exception))
        self.assertEqual((recovery / "tiktok-token-store.key").read_bytes(), generated[0])
        saved = json.loads((recovery / "tiktok-callback.json").read_text())
        self.assertEqual(saved["apps"]["main"]["redirect_url"], "https://new.example.com/callbacks/tiktok-shop")

    def test_existing_app_added_app_live_data_retains_attempt_snapshot_and_rolls_back_original(self):
        self.install()
        self.p(installer.STATE_STORE).write_bytes(b"old state")
        before, key = self.p(installer.CONFIG).read_bytes(), self.p(installer.STORE_KEY).read_bytes()

        def new_callback():
            self.p(installer.STATE_STORE).write_bytes(b"new B state")
            self.p(installer.TOKEN_STORE).write_bytes(b"new B token")

        self.system.live_callback, self.system.fail_health = new_callback, True
        with self.assertRaises(installer.InstallError):
            self.install([{"id": "B", "key": "B-key", "secret": "B-secret"}])
        self.assertEqual(self.p(installer.CONFIG).read_bytes(), before)
        self.assertFalse(self.p("/etc/api-key-hub/secrets/B.secret").exists())
        self.assertEqual(self.p(installer.STATE_STORE).read_bytes(), b"new B state")
        self.assertEqual(self.p(installer.TOKEN_STORE).read_bytes(), b"new B token")
        recovery = self.recovery()
        saved = json.loads((recovery / "tiktok-callback.json").read_text())
        self.assertEqual(set(saved["apps"]), {"main", "B"})
        self.assertEqual((recovery / "secrets/B.secret").read_text(), "B-secret\n")
        self.assertEqual((recovery / "secrets/B.secret").stat().st_mode & 0o777, 0o600)
        self.assertEqual((recovery / "tiktok-token-store.key").read_bytes(), key)

    def test_failed_stop_does_not_restore_or_remove_files_of_running_process(self):
        self.system.fail_health = self.system.stop_fails = True
        with self.assertRaises(installer.InstallError) as failure:
            self.install()
        self.assertIn("回滚", str(failure.exception))
        self.assertTrue(self.system.active)
        for path in (installer.CONFIG, installer.BINARY, installer.UNIT, installer.STORE_KEY, "/etc/api-key-hub/secrets/main.secret"):
            self.assertTrue(self.p(path).exists(), path)

    def test_failed_install_preserves_original_runtime_enablement(self):
        self.install()
        self.system.enabled = False
        self.system.runtime_enabled = self.system.fail_health = True
        with self.assertRaises(installer.InstallError):
            self.install()
        self.assertTrue(self.system.runtime_enabled)
        self.assertFalse(self.system.enabled)
        self.assertIn(["systemctl", "enable", "--runtime", installer.SERVICE], self.system.commands)

    def test_invalid_domain_and_app_do_not_touch_host(self):
        for domain in ("http://example.com", "127.0.0.1", "example.com/path", "x;touch.bad", "example.com:443", "*.example.com", "https://exa\nmple.com"):
            with self.subTest(domain=domain), self.assertRaises(installer.InstallError):
                self.install(domain=domain)
        for app in ({"id": "../escape", "key": "key", "secret": "a"}, {"id": "valid", "key": "key", "secret": "  "}):
            with self.assertRaises(installer.InstallError):
                self.install([app])
        self.assertEqual(self.system.commands, [])

    def test_http_port_collision_and_wrong_binary_fail_before_changes(self):
        self.system.blocked_ports.add(9080)
        with self.assertRaises(installer.InstallError):
            self.install()
        self.assertFalse(self.p(installer.CONFIG).exists())
        self.system.blocked_ports.clear()
        self.binary.write_bytes(b"not ELF")
        with self.assertRaises(installer.InstallError):
            self.install()

    def test_existing_custom_storage_is_rejected(self):
        self.install()
        cfg = self.config()
        cfg["state_store_path"] = "/custom/states"
        self.p(installer.CONFIG).write_text(json.dumps(cfg))
        before = self.p(installer.CONFIG).read_bytes()
        with self.assertRaises(installer.InstallError):
            self.install()
        self.assertEqual(self.p(installer.CONFIG).read_bytes(), before)

    def test_custom_authorize_and_callback_paths_are_rejected_without_overwriting(self):
        self.install()
        original = self.config()
        for field in ("authorize_path", "callback_path"):
            cfg = dict(original, **{field: "/custom"})
            raw = json.dumps(cfg)
            self.p(installer.CONFIG).write_text(raw)
            with self.subTest(field=field), self.assertRaises(installer.InstallError):
                self.install()
            self.assertEqual(self.p(installer.CONFIG).read_text(), raw)

    def test_existing_json_root_must_be_object(self):
        config = self.p(installer.CONFIG)
        config.parent.mkdir(parents=True)
        config.write_text("[]")
        with self.assertRaises(installer.InstallError):
            self.install()
        self.assertEqual(config.read_text(), "[]")

    def test_missing_or_invalid_existing_key_never_resets_tokens(self):
        self.install()
        self.p(installer.TOKEN_STORE).write_bytes(b"must preserve")
        self.p(installer.STORE_KEY).unlink()
        with self.assertRaises(installer.InstallError):
            self.install()
        self.assertFalse(self.p(installer.STORE_KEY).exists())
        self.p(installer.STORE_KEY).write_bytes(b"short key")
        with self.assertRaises(installer.InstallError):
            self.install()
        self.assertEqual(self.p(installer.STORE_KEY).read_bytes(), b"short key")

    def test_official_tls_unit_and_known_dropin_migrate_transactionally_to_http(self):
        self.p(installer.UNIT).parent.mkdir(parents=True)
        self.p(installer.UNIT).write_text(installer.LEGACY_TLS_UNIT)
        old = self.p(installer.LEGACY_OVERRIDE)
        old.parent.mkdir()
        old.write_bytes(installer.CAPABILITIES)
        self.system.active = self.system.enabled = True
        self.system.fail_health = True
        with self.assertRaises(installer.InstallError):
            self.install()
        self.assertEqual(self.p(installer.UNIT).read_text(), installer.LEGACY_TLS_UNIT)
        self.assertEqual(old.read_bytes(), installer.CAPABILITIES)
        self.system.fail_health = False
        self.install()
        self.assertFalse(old.exists())
        self.assertIn(" -http", self.p(installer.UNIT).read_text())

    def test_custom_unit_and_unknown_override_are_rejected(self):
        unit = self.p(installer.UNIT)
        unit.parent.mkdir(parents=True)
        unit.write_text("[Service]\nExecStart=/custom/program\n")
        with self.assertRaises(installer.InstallError):
            self.install()
        unit.unlink()
        custom = self.p(installer.UNIT + ".d/custom.conf")
        custom.parent.mkdir()
        custom.write_text("[Service]\nExecStart=/custom/program\n")
        with self.assertRaises(installer.InstallError):
            self.install()

    def test_secret_target_symlink_cannot_overwrite_unrelated_file(self):
        secret = self.p("/etc/api-key-hub/secrets/main.secret")
        secret.parent.mkdir(parents=True)
        target = self.root / "do-not-change"
        target.write_bytes(b"original")
        secret.symlink_to(target)
        with self.assertRaises(installer.InstallError):
            self.install()
        self.assertEqual(target.read_bytes(), b"original")

    def test_start_failure_restores_service_without_leaking_secret(self):
        self.install()
        original = self.p(installer.CONFIG).read_bytes()
        self.system.fail_restart = True
        with self.assertRaises(installer.InstallError) as failure:
            self.install([{"id": "main", "key": "new", "secret": "TOP_SECRET_VALUE"}])
        self.assertNotIn("TOP_SECRET_VALUE", str(failure.exception))
        self.assertNotIn("TOP_SECRET_VALUE", json.dumps(self.system.commands))
        self.assertEqual(self.p(installer.CONFIG).read_bytes(), original)
        self.assertTrue(self.system.active)

    def test_seller_service_id_is_explicit_and_authorization_requires_it(self):
        cfg = self.install()
        with mock.patch("sys.stdout", new_callable=io.StringIO) as output:
            installer.print_success("callback.example.com", cfg)
        self.assertNotIn("/authorize?", output.getvalue())
        self.assertIn("--service-id", output.getvalue())
        cfg = self.install(service_id="actual-service-id")
        self.assertEqual(cfg["apps"]["main"]["authorize_params"], {"service_id": "actual-service-id"})
        with mock.patch("sys.stdout", new_callable=io.StringIO) as output:
            installer.print_success("callback.example.com", cfg)
        self.assertIn("/authorize?app_id=main", output.getvalue())
        cfg["apps"]["main"]["authorize_params"]["scope"] = "seller.read"
        self.p(installer.CONFIG).write_text(json.dumps(cfg))
        cfg = self.install(service_id="updated-service-id")
        self.assertEqual(cfg["apps"]["main"]["authorize_params"], {"service_id": "updated-service-id", "scope": "seller.read"})

    def test_null_authorize_params_accepts_updates_and_success_output(self):
        self.install()
        for service_id in (None, "service-123"):
            cfg = self.config()
            cfg["apps"]["main"]["authorize_params"] = None
            self.p(installer.CONFIG).write_text(json.dumps(cfg))
            cfg = self.install(service_id=service_id)
            with mock.patch("sys.stdout", new_callable=io.StringIO) as output:
                installer.print_success("callback.example.com", cfg)
            if service_id:
                self.assertEqual(cfg["apps"]["main"]["authorize_params"], {"service_id": service_id})
                self.assertIn("/authorize?", output.getvalue())
            else:
                self.assertNotIn("/authorize?", output.getvalue())

    def test_real_local_http_health_checks_healthz(self):
        seen = []

        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                seen.append(self.path)
                self.send_response(200 if self.path == "/healthz" else 404)
                self.end_headers()

            def log_message(self, *args):
                pass

        server = HTTPServer(("127.0.0.1", 0), Handler)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        installer.System().health("127.0.0.1", server.server_port)
        self.assertEqual(seen, ["/healthz"])

    def test_real_http_health_rejects_404_500_and_refused_connection(self):
        response_code = 404

        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                self.send_response(response_code)
                self.end_headers()

            def log_message(self, *args):
                pass

        server = HTTPServer(("127.0.0.1", 0), Handler)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        with mock.patch.object(installer.time, "sleep"):
            for response_code in (404, 500):
                with self.subTest(status=response_code), self.assertRaises(installer.InstallError):
                    installer.System().health("127.0.0.1", server.server_port)
            with socket.socket() as unavailable:
                unavailable.bind(("127.0.0.1", 0))
                with self.assertRaises(installer.InstallError):
                    installer.System().health("127.0.0.1", unavailable.getsockname()[1])


if __name__ == "__main__":
    unittest.main()
