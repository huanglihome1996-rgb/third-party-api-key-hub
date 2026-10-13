#!/usr/bin/env python3
"""Install an HTTP callback behind an external HTTPS load balancer."""
import argparse
import fcntl
import getpass
import hashlib
import http.client
import ipaddress
import json
import os
from pathlib import Path
import platform
import re
import secrets
import shutil
import socket
import stat
import struct
import subprocess
import sys
import tempfile
import time
import warnings
from urllib.parse import urlsplit, urlencode

SERVICE = "api-key-hub-ttcallback.service"
CONFIG = "/etc/api-key-hub/tiktok-callback.json"
STORE_KEY = "/etc/api-key-hub/tiktok-token-store.key"
STATE_STORE = "/var/lib/api-key-hub/tiktok-states.json"
TOKEN_STORE = "/var/lib/api-key-hub/tiktok-tokens.enc.json"
BINARY = "/opt/api-key-hub/bin/ttcallback"
UNIT = "/etc/systemd/system/" + SERVICE
OVERRIDE = UNIT + ".d/https.conf"
LEGACY_OVERRIDE = UNIT + ".d/https-port.conf"
ACCOUNT = "api-key-hub"
RECOVERY_ROOT = "/var/backups/api-key-hub-ttcallback"
CAPABILITIES = b"[Service]\nAmbientCapabilities=CAP_NET_BIND_SERVICE\nCapabilityBoundingSet=CAP_NET_BIND_SERVICE\n"
DEFAULT_UNIT = """[Unit]
Description=Third-Party API Key Hub TikTok HTTP callback
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=api-key-hub
Group=api-key-hub
EnvironmentFile=-/etc/api-key-hub/tiktok-callback.env
ExecStart=/opt/api-key-hub/bin/ttcallback -config /etc/api-key-hub/tiktok-callback.json -http
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/api-key-hub
UMask=0077

[Install]
WantedBy=multi-user.target
"""
LEGACY_TLS_UNIT = DEFAULT_UNIT.replace("TikTok HTTP callback", "TikTok callback").replace(
    " -http\n", " -tls-cert /etc/api-key-hub/tls/fullchain.pem -tls-key /etc/api-key-hub/tls/privkey.pem\n")


class InstallError(Exception):
    pass


def normalize_domain(value):
    value = value.strip().lower()
    if any(ord(c) < 32 or ord(c) == 127 for c in value):
        raise InstallError("回调域名不能包含控制字符。")
    if "://" in value:
        url = urlsplit(value)
        if url.scheme != "https" or url.path or url.query or url.fragment:
            raise InstallError("域名仅接受裸域名或 https://域名，不允许路径和端口。")
        value = url.netloc
    if len(value) > 253 or len(value.split(".")) < 2 or any(
        not re.fullmatch(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?", label) for label in value.split(".")
    ):
        raise InstallError("请输入公网 HTTPS 回调域名，不允许 IP、路径、端口或特殊字符。")
    try:
        ipaddress.ip_address(value)
    except ValueError:
        return value
    raise InstallError("公网回调必须使用 DNS 域名，不能使用 IP。")


def parse_listen(value):
    try:
        host, port = value.rsplit(":", 1)
        address = ipaddress.ip_address(host)
        if not port.isascii() or not port.isdigit():
            raise ValueError
        port = int(port)
        if address.version != 4 or not 1024 <= port <= 65535:
            raise ValueError
    except ValueError:
        raise InstallError("--listen-addr 必须为 IPv4:端口，端口范围1024至65535。") from None
    return host, port


def validate_apps(apps):
    if not apps:
        raise InstallError("至少需要一个 App。")
    seen = set()
    for app in apps:
        if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_-]{0,63}", app["id"]) or app["id"] in seen:
            raise InstallError("App id 必须唯一，只能包含字母、数字、下划线或连字符（最多64字符）。")
        seen.add(app["id"])
        for field in ("key", "secret"):
            value = app[field]
            if not value.strip() or len(value) > 8192 or any(ord(c) < 32 or ord(c) == 127 for c in value):
                raise InstallError("App key/secret 不能为空或包含控制字符。")


class System:
    def run(self, args, check=True):
        try:
            result = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        except OSError:
            raise InstallError("系统命令无法执行：" + args[0]) from None
        if check and result.returncode:
            raise InstallError("系统命令失败：" + " ".join(args[:2]) + "；请检查系统配置。")
        return result.stdout.strip()

    def which(self, name):
        return shutil.which(name)

    def port_available(self, host, port):
        try:
            with socket.socket(socket.AF_INET) as listener:
                listener.bind((host, port))
        except OSError:
            return False
        return True

    def service_owns_port(self, port):
        if not self.which("ss"):
            return False
        pid = self.run(["systemctl", "show", SERVICE, "--property=MainPID", "--value"], check=False)
        lines = self.run(["ss", "-H", "-ltnp", "sport = :" + str(port)], check=False).splitlines()
        return pid.isdigit() and pid != "0" and bool(lines) and all("pid=" + pid + "," in line for line in lines)

    def health(self, host, port):
        for attempt in range(12):
            connection = http.client.HTTPConnection(host, port, timeout=3)
            try:
                connection.request("GET", "/healthz")
                if connection.getresponse().status == 200:
                    return
            except (OSError, http.client.HTTPException):
                pass
            finally:
                connection.close()
            if attempt < 11:
                time.sleep(1)
        raise InstallError("本机 HTTP /healthz 检查失败。")


class Files:
    """Snapshot installer-owned files and directory permissions, never live data."""
    def __init__(self):
        self.before = {}
        self.directories = {}

    @staticmethod
    def safe(path):
        for parent in (path, *path.parents):
            if parent.is_symlink():
                raise InstallError("安装目标包含符号链接，拒绝自动覆盖。")
        if path.exists() and not path.is_file():
            raise InstallError("安装目标必须是普通文件。")

    def remember(self, path):
        self.safe(path)
        if path not in self.before:
            info = path.stat() if path.exists() else None
            self.before[path] = (path.read_bytes(), stat.S_IMODE(info.st_mode), info.st_uid, info.st_gid) if info else None

    def write(self, path, data, mode=0o600, uid=0, gid=0):
        self.remember(path)
        self.atomic(path, data, mode, uid, gid)

    def remove(self, path):
        self.remember(path)
        path.unlink(missing_ok=True)

    def directory(self, path, uid, gid, mode=0o750):
        if any(parent.is_symlink() for parent in (path, *path.parents)):
            raise InstallError("安装目录包含符号链接。")
        if path not in self.directories:
            info = path.stat() if path.exists() else None
            self.directories[path] = (stat.S_IMODE(info.st_mode), info.st_uid, info.st_gid) if info else None
        path.mkdir(parents=True, exist_ok=True)
        os.chown(path, uid, gid)
        os.chmod(path, mode)

    @staticmethod
    def atomic(path, data, mode, uid, gid):
        path.parent.mkdir(parents=True, exist_ok=True)
        descriptor, temporary = tempfile.mkstemp(prefix=".callback-", dir=path.parent)
        try:
            with os.fdopen(descriptor, "wb") as output:
                output.write(data)
                output.flush()
                os.fsync(output.fileno())
                os.fchmod(output.fileno(), mode)
                os.fchown(output.fileno(), uid, gid)
            os.replace(temporary, path)
        finally:
            if os.path.exists(temporary):
                os.unlink(temporary)

    def restore(self):
        for path, previous in reversed(list(self.before.items())):
            if previous is None:
                path.unlink(missing_ok=True)
            else:
                self.atomic(path, *previous)
        for path, previous in reversed(list(self.directories.items())):
            if previous:
                mode, uid, gid = previous
                os.chown(path, uid, gid)
                os.chmod(path, mode)
            else:
                try:
                    path.rmdir()
                except OSError:
                    pass  # Runtime Token/state files must survive rollback.


class Installer:
    def __init__(self, root=Path("/"), system=None):
        self.root = Path(root)
        self.system = system or System()

    def path(self, value):
        return self.root / value.lstrip("/")

    def status(self, operation):
        return self.system.run(["systemctl", operation, SERVICE], check=False)

    def existing_config(self):
        path = self.path(CONFIG)
        Files.safe(path)
        if not path.exists():
            if self.path(TOKEN_STORE).exists() or self.path(STATE_STORE).exists():
                raise InstallError("检测到已有 Token/state 但配置缺失，拒绝自动接管。")
            return {"apps": {}}
        try:
            cfg = json.loads(path.read_text())
        except (ValueError, OSError):
            raise InstallError("已有配置无法读取，拒绝覆盖。") from None
        if not isinstance(cfg, dict):
            raise InstallError("已有配置必须是 JSON 对象，拒绝覆盖。")
        for key, expected in (("state_store_path", STATE_STORE), ("token_store_path", TOKEN_STORE), ("token_store_key_file", STORE_KEY)):
            if cfg.get(key) != expected:
                raise InstallError("已有自定义 Token/state/key 路径，请使用高级部署。")
        for key, expected in (("authorize_path", "/authorize"), ("callback_path", "/callbacks/tiktok-shop")):
            if cfg.get(key, expected) != expected:
                raise InstallError("已有自定义授权/回调路径，请使用高级部署。")
        if not isinstance(cfg.get("apps"), dict):
            raise InstallError("已有配置的 apps 无效。")
        for app_id, app in cfg["apps"].items():
            if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_-]{0,63}", app_id) or not isinstance(app, dict):
                raise InstallError("已有 App 配置格式不受自动安装支持。")
            if app.get("app_secret_file") != "/etc/api-key-hub/secrets/" + app_id + ".secret" or app.get("app_secret_env"):
                raise InstallError("已有 App 使用自定义 secret 路径或环境变量，请使用高级部署。")
        return cfg

    def preflight(self, binary, host, port):
        release = self.path("/etc/os-release").read_text()
        if not re.search(r'^ID=["\']?ubuntu["\']?$', release, re.M) or not re.search(r'^VERSION_ID=["\']?24\.04["\']?$', release, re.M):
            raise InstallError("自动安装仅支持 Ubuntu 24.04。")
        if not self.path("/run/systemd/system").is_dir() or not self.system.which("systemctl"):
            raise InstallError("需要运行中的 systemd。")
        dropins = self.path(UNIT + ".d")
        if dropins.exists():
            for item in dropins.iterdir():
                if item.name not in ("https.conf", "https-port.conf") or item.read_bytes().replace(b"\r\n", b"\n").strip() != CAPABILITIES.strip():
                    raise InstallError("已有自定义 systemd override，请使用高级部署。")
        effective = self.system.run(["systemctl", "show", SERVICE, "--property=DropInPaths", "--value"], check=False)
        if any(name not in (OVERRIDE, LEGACY_OVERRIDE) for name in effective.split()):
            raise InstallError("检测到其他 systemd override，请使用高级部署。")
        unit = self.path(UNIT)
        if unit.exists() and unit.read_bytes().replace(b"\r\n", b"\n").strip() not in (DEFAULT_UNIT.encode().strip(), LEGACY_TLS_UNIT.encode().strip()):
            raise InstallError("已有自定义 systemd unit，请使用高级部署。")
        machine = {"x86_64": 62, "aarch64": 183}.get(platform.machine())
        try:
            with Path(binary).open("rb") as source:
                header = source.read(20)
        except OSError:
            raise InstallError("未找到 ttcallback，请在完整安装包内运行或提供 --binary。") from None
        if not machine or len(header) < 20 or header[:6] != b"\x7fELF\x02\x01" or struct.unpack("<H", header[18:20])[0] != machine:
            raise InstallError("ttcallback 不是匹配本机架构的 Linux amd64/arm64 程序。")
        if not self.system.port_available(host, port) and not self.system.service_owns_port(port):
            raise InstallError("监听端口被其他服务占用；安装器不会停止其他服务。")
        for path in (STORE_KEY, STATE_STORE, TOKEN_STORE):
            Files.safe(self.path(path))
        key = self.path(STORE_KEY)
        if key.exists() and len(key.read_bytes()) != 32:
            raise InstallError("已有加密 key 必须恰好32原始字节；拒绝重置。")
        if self.path(TOKEN_STORE).exists() and not key.exists():
            raise InstallError("已有加密 Token 但 key 缺失；请先恢复原 key。")

    def account(self):
        group = self.system.run(["getent", "group", ACCOUNT], check=False)
        if not group:
            self.system.run(["groupadd", "--system", ACCOUNT])
            group = self.system.run(["getent", "group", ACCOUNT])
        parts = group.split(":")
        if len(parts) < 3 or not parts[2].isdigit():
            raise InstallError("无法确定 api-key-hub 组的 gid。")
        if not self.system.run(["id", "-u", ACCOUNT], check=False).isdigit():
            self.system.run(["useradd", "--system", "--gid", ACCOUNT, "--home-dir", "/var/lib/api-key-hub", "--no-create-home", "--shell", "/usr/sbin/nologin", ACCOUNT])
        return int(self.system.run(["id", "-u", ACCOUNT])), int(parts[2])

    def live_data_signature(self):
        result = {}
        for name in (TOKEN_STORE, STATE_STORE):
            path = self.path(name)
            if path.exists():
                with path.open("rb") as source:
                    result[name] = hashlib.file_digest(source, "sha256").digest()
            else:
                result[name] = None
        return result

    def recovery_snapshot(self, cfg):
        saved = Files()
        parent = self.path(RECOVERY_ROOT)
        saved.directory(parent, 0, 0, 0o700)
        recovery = Path(tempfile.mkdtemp(prefix="attempt-", dir=parent))
        saved.directory(recovery, 0, 0, 0o700)
        saved.write(recovery / "tiktok-callback.json", (json.dumps(cfg, indent=2) + "\n").encode())
        saved.write(recovery / "tiktok-token-store.key", self.path(STORE_KEY).read_bytes())
        saved.directory(recovery / "secrets", 0, 0, 0o700)
        for app_id, app in cfg["apps"].items():
            saved.write(recovery / "secrets" / (app_id + ".secret"), self.path(app["app_secret_file"]).read_bytes())
        return recovery

    def restore_enablement(self, previous):
        self.system.run(["systemctl", "disable", SERVICE], check=False)
        if previous == "enabled-runtime":
            self.system.run(["systemctl", "enable", "--runtime", SERVICE])
        elif previous == "enabled":
            self.system.run(["systemctl", "enable", SERVICE])

    def install(self, domain, apps, *, binary, listen_addr="0.0.0.0:9080", service_id=None):
        domain = normalize_domain(domain)
        validate_apps(apps)
        host, port = parse_listen(listen_addr)
        if service_id is not None and not re.fullmatch(r"[A-Za-z0-9_-]{1,128}", service_id):
            raise InstallError("service_id 格式无效。")
        cfg = self.existing_config()
        self.preflight(binary, host, port)
        active, enabled = self.status("is-active") == "active", self.status("is-enabled")
        live_before = self.live_data_signature()
        uid, gid = self.account()
        files = Files()
        try:
            files.directory(self.path("/etc/api-key-hub"), 0, gid)
            files.directory(self.path("/etc/api-key-hub/secrets"), uid, gid, 0o700)
            files.directory(self.path("/var/lib/api-key-hub"), uid, gid, 0o700)
            files.directory(self.path("/opt/api-key-hub"), 0, 0, 0o755)
            files.directory(self.path("/opt/api-key-hub/bin"), 0, 0, 0o755)
            changed = {app["id"] for app in apps}
            for app_id, item in cfg["apps"].items():
                if app_id not in changed:
                    secret = self.path(item["app_secret_file"])
                    if not secret.exists() or not secret.read_bytes().strip():
                        raise InstallError("未修改的 App secret 缺失或为空，请先恢复。")
                    files.write(secret, secret.read_bytes(), uid=uid, gid=gid)
            for app in apps:
                secret_path = "/etc/api-key-hub/secrets/" + app["id"] + ".secret"
                files.write(self.path(secret_path), (app["secret"] + "\n").encode(), uid=uid, gid=gid)
                item = {"app_key": app["key"], "app_secret_file": secret_path,
                        "authorize_url": "https://services.us.tiktokshop.com/open/authorize",
                        "token_url": "https://auth.tiktok-shops.com/api/v2/token/get"}
                previous = cfg["apps"].get(app["id"], {})
                for field in ("authorize_url", "authorize_params", "token_url"):
                    if field in previous:
                        item[field] = previous[field]
                if service_id is not None:
                    item["authorize_params"] = dict(item.get("authorize_params") or {}, service_id=service_id)
                cfg["apps"][app["id"]] = item
            cfg.update(listen_addr=listen_addr, authorize_path="/authorize", callback_path="/callbacks/tiktok-shop",
                       state_store_path=STATE_STORE, token_store_path=TOKEN_STORE, token_store_key_file=STORE_KEY)
            for item in cfg["apps"].values():
                item["redirect_url"] = "https://" + domain + "/callbacks/tiktok-shop"
            files.write(self.path(CONFIG), (json.dumps(cfg, indent=2) + "\n").encode(), uid=uid, gid=gid)
            key = self.path(STORE_KEY)
            files.write(key, key.read_bytes() if key.exists() else secrets.token_bytes(32), uid=uid, gid=gid)
            files.write(self.path(BINARY), Path(binary).read_bytes(), 0o755)
            files.write(self.path(UNIT), DEFAULT_UNIT.encode(), 0o644)
            files.remove(self.path(OVERRIDE))
            files.remove(self.path(LEGACY_OVERRIDE))
            self.system.run(["systemctl", "daemon-reload"])
            self.system.run(["systemctl", "enable", SERVICE])
            self.system.run(["systemctl", "restart", SERVICE])
            if self.status("is-active") != "active":
                raise InstallError("callback 服务未启动。")
            self.system.health("127.0.0.1" if host == "0.0.0.0" else host, port)
        except BaseException as error:
            recovery = None
            try:
                self.system.run(["systemctl", "stop", SERVICE], check=False)
                if self.status("is-active") not in ("inactive", "failed", "unknown"):
                    raise InstallError("callback 进程仍在运行，拒绝恢复或删除其文件。")
                live_after = self.live_data_signature()
                if live_after != live_before:
                    recovery = self.recovery_snapshot(cfg)
                # A newly generated encryption key must survive any live Token.
                key = self.path(STORE_KEY)
                if key in files.before and files.before[key] is None and self.path(TOKEN_STORE).exists():
                    del files.before[key]
                # A first run may have accepted callbacks before health failed.
                # Retain their matching key/config/secrets so data stays recoverable.
                live_data = self.path(CONFIG) in files.before and files.before[self.path(CONFIG)] is None and any(value is not None for value in live_after.values())
                if live_data:
                    for path in list(files.before):
                        if path in (self.path(CONFIG), self.path(STORE_KEY)) or path.parent == self.path("/etc/api-key-hub/secrets"):
                            del files.before[path]
                files.restore()
                self.system.run(["systemctl", "daemon-reload"])
                self.restore_enablement(enabled)
                self.system.run(["systemctl", "restart" if active else "stop", SERVICE], check=active)
            except Exception:
                detail = "；恢复快照：" + str(recovery) if recovery else ""
                raise InstallError("安装失败且自动回滚未完成，已保留尝试文件；请检查 systemctl status api-key-hub-ttcallback" + detail + "。") from None
            if live_data:
                raise InstallError("安装失败；运行期间产生的数据及其配置/secret/key已保留，修复环境后重跑；恢复快照：" + str(recovery)) from None
            if recovery:
                raise InstallError("安装失败，原配置已恢复；实时数据未改动，尝试配置/secret/key恢复快照：" + str(recovery)) from None
            if isinstance(error, InstallError):
                raise
            raise InstallError("安装中止，原服务文件和运行状态已恢复。") from None
        return cfg


def print_success(domain, cfg):
    print("HTTP callback 已启动，监听：" + cfg["listen_addr"])
    print("请将 ALB 的 HTTPS 域名转发至本机上述 HTTP 地址，健康检查路径 /healthz。")
    print("公网回调：https://" + domain + "/callbacks/tiktok-shop（请在 TikTok 后台登记）")
    print("域名修改后需同步更新所有 App 的 TikTok 后台配置。")
    for app_id, item in cfg["apps"].items():
        if (item.get("authorize_params") or {}).get("service_id"):
            print("App " + app_id + " Seller 授权入口：https://" + domain + "/authorize?" + urlencode({"app_id": app_id}))
        else:
            print("App " + app_id + "：callback 已配置；Seller 授权还需重跑时提供 --service-id，app_id 不能替代 service_id。")
    print("管理：sudo systemctl status api-key-hub-ttcallback")
    print("日志：sudo journalctl -u api-key-hub-ttcallback -n 50 --no-pager")


def main():
    parser = argparse.ArgumentParser(description="一次配置并启动 Ubuntu 24.04 HTTP callback；公网 HTTPS 由外部 ALB 终止。")
    parser.add_argument("--binary", type=Path, help="已有支持 -http 的 Linux ttcallback 二进制路径")
    parser.add_argument("--listen-addr", default="0.0.0.0:9080", help="HTTP IPv4:端口（默认0.0.0.0:9080，端口>=1024）")
    parser.add_argument("--service-id", help="可选 Seller service_id；独立于 app_id")
    args = parser.parse_args()
    if os.geteuid() != 0:
        parser.error("请使用 sudo bash install.sh 执行。")
    if not sys.stdin.isatty():
        parser.error("需要交互终端，secret 不接受管道或命令行输入。")
    with open("/run/lock/api-key-hub-ttcallback-install.lock", "a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise InstallError("另一个安装任务正在运行。") from None
        domain = normalize_domain(input("公网 HTTPS callback 域名（裸域名或 https://域名）："))
        apps = []
        while True:
            with warnings.catch_warnings():
                warnings.simplefilter("error", getpass.GetPassWarning)
                try:
                    app = {"id": input("app_id：").strip(), "key": input("app_key：").strip(), "secret": getpass.getpass("app_secret（隐藏输入）：")}
                except getpass.GetPassWarning:
                    raise InstallError("无法隐藏 secret 输入，请在交互终端运行。") from None
            validate_apps(apps + [app])
            apps.append(app)
            if input("继续添加另一个 App？[y/N]：").strip().lower() not in ("y", "yes"):
                break
        binary = args.binary or Path(__file__).resolve().parents[2] / "bin/ttcallback"
        print("正在检查环境、安装配置并启动 HTTP 服务……")
        cfg = Installer().install(domain, apps, binary=binary, listen_addr=args.listen_addr, service_id=args.service_id)
        print_success(domain, cfg)


if __name__ == "__main__":
    try:
        main()
    except (InstallError, OSError, ValueError, EOFError, KeyboardInterrupt) as error:
        message = str(error) if isinstance(error, InstallError) else "安装中止；请检查输入和文件权限后重试。"
        print("错误：" + message, file=sys.stderr)
        sys.exit(1)
