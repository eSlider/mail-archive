#!/usr/bin/env python3
"""Run sync for a single account by config filename.

Usage:
    python sync_one.py eslider-gmail
    python sync_one.py eslider-gmail.yml

Environment: EMAILS_DIR, CONFIG_DIR, SECRETS_DIR (defaults to project dirs)
"""

from __future__ import annotations

import os
import sys
from pathlib import Path

# Project root (parent of scripts/)
SCRIPT_DIR = Path(__file__).resolve().parent
PROJECT_ROOT = SCRIPT_DIR.parent

BASE_DIR = Path(os.getenv("EMAILS_DIR", str(PROJECT_ROOT / "emails")))
CONFIG_DIR = Path(os.getenv("CONFIG_DIR", str(PROJECT_ROOT / "config")))
SECRETS_DIR = Path(os.getenv("SECRETS_DIR", str(PROJECT_ROOT / "secrets")))


def main() -> int:
    if len(sys.argv) < 2:
        print("Usage: python sync_one.py <account>", file=sys.stderr)
        print("  account: config filename without or with .yml (e.g. eslider-gmail)", file=sys.stderr)
        return 1

    name = sys.argv[1].removesuffix(".yml")
    config_path = CONFIG_DIR / f"{name}.yml"

    if not config_path.exists():
        print(f"Config not found: {config_path}", file=sys.stderr)
        return 1

    sys.path.insert(0, str(SCRIPT_DIR))
    from config_loader import load_account_config
    from sync_gmail_api import sync_gmail_api_account
    from sync_imap import sync_imap_account
    from sync_pop3 import sync_pop3_account

    cfg = load_account_config(config_path)
    if cfg is None:
        return 1

    account_name = cfg["_account_name"]
    account_type = cfg["type"]

    try:
        if account_type == "IMAP":
            n = sync_imap_account(account_name, cfg, BASE_DIR)
        elif account_type == "POP3":
            n = sync_pop3_account(account_name, cfg, BASE_DIR)
        elif account_type == "GMAIL_API":
            n = sync_gmail_api_account(account_name, cfg, BASE_DIR, SECRETS_DIR)
        else:
            print(f"Unknown account type: {account_type}", file=sys.stderr)
            return 1

        print(f"Sync complete: {n} new messages")
        return 0
    except Exception:
        import traceback

        traceback.print_exc()
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
