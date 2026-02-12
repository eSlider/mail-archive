#!/usr/bin/env python3
"""List all IMAP folders for an account. Use to discover available folders."""

from __future__ import annotations

import os
import sys
from pathlib import Path

import yaml
from imapclient import IMAPClient

SCRIPT_DIR = Path(__file__).resolve().parent
PROJECT_ROOT = SCRIPT_DIR.parent
CONFIG_DIR = Path(os.getenv("CONFIG_DIR", str(PROJECT_ROOT / "config")))


def main() -> int:
    if len(sys.argv) < 2:
        print("Usage: python list_imap_folders.py <account>", file=sys.stderr)
        print("  e.g. list_imap_folders.py eslider-gmail", file=sys.stderr)
        return 1

    name = sys.argv[1].removesuffix(".yml")
    config_path = CONFIG_DIR / f"{name}.yml"
    if not config_path.exists():
        print(f"Config not found: {config_path}", file=sys.stderr)
        return 1

    with open(config_path) as f:
        cfg = yaml.safe_load(f)

    if cfg.get("type") != "IMAP":
        print("Account is not IMAP type", file=sys.stderr)
        return 1

    host = cfg["host"]
    port = cfg.get("port", 993)
    use_ssl = cfg.get("ssl", True)
    user = cfg["email"]
    password = cfg["password"]

    print(f"Connecting to {host} as {user}...")
    with IMAPClient(host, port=port, ssl=use_ssl) as client:
        client.login(user, password)
        folders = client.list_folders()

    print(f"\nFound {len(folders)} folders:\n")
    for flags, delimiter, folder_name in sorted(folders, key=lambda x: x[2].lower()):
        flag_str = " ".join(f.decode() if isinstance(f, bytes) else str(f) for f in flags)
        print(f"  {folder_name}")
        if flag_str:
            print(f"    flags: {flag_str}")

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
