"""POP3 email sync: connects to POP3 server, downloads messages as .eml files."""

from __future__ import annotations

import email
import email.utils
import hashlib
import logging
import poplib
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from eml_utils import content_checksum, set_file_received_time

logger = logging.getLogger(__name__)


def _parse_date(raw_msg: bytes) -> datetime:
    """Extract date from email headers, fallback to now()."""
    msg = email.message_from_bytes(raw_msg)
    date_str = msg.get("Date", "")
    if date_str:
        parsed = email.utils.parsedate_to_datetime(date_str)
        return parsed.astimezone(timezone.utc)
    return datetime.now(timezone.utc)


def _msg_hash(raw_msg: bytes) -> str:
    """Generate a short unique hash for deduplication (POP3 has no stable UIDs)."""
    return hashlib.sha256(raw_msg).hexdigest()[:16]


def _load_synced_hashes(state_file: Path) -> set[str]:
    """Load previously synced message hashes."""
    if not state_file.exists():
        return set()
    text = state_file.read_text().strip()
    if not text:
        return set()
    return {h for h in text.split("\n") if h.strip()}


def _save_synced_hashes(state_file: Path, hashes: set[str]) -> None:
    """Persist synced hashes to state file."""
    state_file.parent.mkdir(parents=True, exist_ok=True)
    state_file.write_text("\n".join(sorted(hashes)))


def sync_pop3_account(account_name: str, cfg: dict[str, Any], base_dir: Path) -> int:
    """
    Sync emails from a single POP3 account.

    Returns the number of newly downloaded messages.
    """
    host: str = cfg["host"]
    port: int = cfg.get("port", 995)
    use_ssl: bool = cfg.get("ssl", True)
    user: str = cfg["email"]
    password: str = cfg["password"]

    account_dir = base_dir / account_name
    inbox_dir = account_dir / "inbox"
    state_file = inbox_dir / ".sync_state"
    old_state = account_dir / ".sync_state"
    # Migrate legacy .sync_state to inbox/
    if not state_file.exists() and old_state.exists():
        inbox_dir.mkdir(parents=True, exist_ok=True)
        state_file.write_text(old_state.read_text())
    synced_hashes = _load_synced_hashes(state_file)
    total_new = 0

    logger.info("Connecting POP3 to %s:%d (SSL=%s) as %s", host, port, use_ssl, user)

    if use_ssl:
        server = poplib.POP3_SSL(host, port)
    else:
        server = poplib.POP3(host, port)

    try:
        server.user(user)
        server.pass_(password)
        logger.info("POP3 logged in to %s", host)

        num_messages = len(server.list()[1])
        logger.info("POP3 mailbox has %d messages", num_messages)

        for i in range(1, num_messages + 1):
            raw_lines = server.retr(i)[1]
            raw_msg = b"\r\n".join(raw_lines)

            msg_h = _msg_hash(raw_msg)
            if msg_h in synced_hashes:
                continue

            msg_date = _parse_date(raw_msg)
            checksum = content_checksum(raw_msg)
            filename = f"{checksum}-{msg_h}.eml"
            filepath = inbox_dir / filename

            inbox_dir.mkdir(parents=True, exist_ok=True)
            filepath.write_bytes(raw_msg)
            set_file_received_time(filepath, msg_date)
            synced_hashes.add(msg_h)
            total_new += 1

        _save_synced_hashes(state_file, synced_hashes)
    finally:
        server.quit()

    logger.info("POP3 account '%s': downloaded %d new messages", account_name, total_new)
    return total_new
