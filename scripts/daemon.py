"""Main daemon: loads configs, schedules periodic sync for each account."""

from __future__ import annotations

import logging
import os
import signal
import sys
import time
from pathlib import Path

import schedule

from config_loader import load_all_configs
from sync_gmail_api import sync_gmail_api_account
from sync_imap import sync_imap_account
from sync_pop3 import sync_pop3_account

# Paths
BASE_DIR = Path(os.getenv("EMAILS_DIR", "/app/emails"))
CONFIG_DIR = Path(os.getenv("CONFIG_DIR", "/app/config"))
SECRETS_DIR = Path(os.getenv("SECRETS_DIR", "/app/secrets"))

# Logging
log_level = os.getenv("LOG_LEVEL", "INFO").upper()
logging.basicConfig(
    level=getattr(logging, log_level, logging.INFO),
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    datefmt="%Y-%m-%d %H:%M:%S",
    stream=sys.stdout,
)
logger = logging.getLogger("mail-sync")

# Graceful shutdown
shutdown_requested = False


def _handle_signal(_signum: int, _frame: object) -> None:
    global shutdown_requested  # noqa: PLW0603
    logger.info("Shutdown signal received, stopping...")
    shutdown_requested = True


signal.signal(signal.SIGTERM, _handle_signal)
signal.signal(signal.SIGINT, _handle_signal)


def run_sync(cfg: dict) -> int:
    """Run sync for a single account, returning new message count.

    Unlike sync_account(), this raises exceptions instead of swallowing them,
    so the caller (sync API) can report errors to the UI.
    """
    account_name: str = cfg["_account_name"]
    account_type: str = cfg["type"]

    if account_type == "IMAP":
        return sync_imap_account(account_name, cfg, BASE_DIR)
    elif account_type == "POP3":
        return sync_pop3_account(account_name, cfg, BASE_DIR)
    elif account_type == "GMAIL_API":
        return sync_gmail_api_account(account_name, cfg, BASE_DIR, SECRETS_DIR)
    else:
        raise ValueError(f"Unknown account type: {account_type}")


def sync_account(cfg: dict) -> None:
    """Run sync for a single account based on its type (exception-safe for scheduler)."""
    try:
        run_sync(cfg)
    except Exception:
        logger.exception("Failed to sync account '%s'", cfg["_account_name"])


def main() -> None:
    """Entry point: load configs, schedule jobs, run loop."""
    logger.info("Starting mail-sync daemon")
    logger.info("Config dir : %s", CONFIG_DIR)
    logger.info("Emails dir : %s", BASE_DIR)
    logger.info("Secrets dir: %s", SECRETS_DIR)

    BASE_DIR.mkdir(parents=True, exist_ok=True)

    configs = load_all_configs(CONFIG_DIR)
    if not configs:
        logger.warning("No account configs found in %s — waiting for configs...", CONFIG_DIR)

    # Start sync trigger API in background thread.
    from sync_api import start_sync_api

    api_port = int(os.getenv("SYNC_API_PORT", "8081"))
    start_sync_api(configs, run_sync, port=api_port)

    # Schedule each account
    for cfg in configs:
        name = cfg["_account_name"]
        interval_sec = cfg["_interval_seconds"]
        logger.info(
            "Scheduling '%s' (%s) every %d seconds",
            name,
            cfg["type"],
            interval_sec,
        )
        schedule.every(interval_sec).seconds.do(sync_account, cfg=cfg)

        # Run initial sync immediately
        sync_account(cfg)

    # Main loop
    while not shutdown_requested:
        schedule.run_pending()
        time.sleep(1)

    logger.info("Daemon stopped")


if __name__ == "__main__":
    main()
