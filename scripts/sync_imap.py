"""IMAP email sync: connects to IMAP server, downloads new messages as .eml files."""

from __future__ import annotations

import email
import email.utils
import logging
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from imapclient import IMAPClient

from eml_utils import content_checksum, set_file_received_time
from folder_mapping import imap_folder_to_path

logger = logging.getLogger(__name__)


def _parse_date(raw_msg: bytes) -> datetime:
    """Extract date from email headers, fallback to now()."""
    from eml_utils import parse_date_from_bytes

    return parse_date_from_bytes(raw_msg)


def _load_synced_uids(state_file: Path) -> set[int]:
    """Load previously synced UIDs from state file."""
    if not state_file.exists():
        return set()
    text = state_file.read_text().strip()
    if not text:
        return set()
    return {int(uid) for uid in text.split("\n") if uid.strip()}


def _save_synced_uids(state_file: Path, uids: set[int]) -> None:
    """Persist synced UIDs to state file."""
    state_file.parent.mkdir(parents=True, exist_ok=True)
    state_file.write_text("\n".join(str(uid) for uid in sorted(uids)))


def sync_imap_account(account_name: str, cfg: dict[str, Any], base_dir: Path) -> int:
    """
    Sync emails from a single IMAP account.

    Returns the number of newly downloaded messages.
    """
    host: str = cfg["host"]
    port: int = cfg.get("port", 993)
    use_ssl: bool = cfg.get("ssl", True)
    user: str = cfg["email"]
    password: str = cfg["password"]
    folders_cfg = cfg.get("folders", ["INBOX"])

    account_dir = base_dir / account_name
    total_new = 0

    logger.info("Connecting to %s:%d (SSL=%s) as %s", host, port, use_ssl, user)

    with IMAPClient(host, port=port, ssl=use_ssl) as client:
        client.login(user, password)
        logger.info("Logged in successfully to %s", host)

        if folders_cfg == "all" or (
            isinstance(folders_cfg, list) and "all" in folders_cfg
        ):
            raw_folders = client.list_folders()
            # Skip \Noselect (parent-only folders that cannot be selected)
            folders = [
                name
                for flags, _delim, name in raw_folders
                if not any("noselect" in str(f).lower() for f in flags)
            ]
            logger.info("Auto-discovered %d syncable folders", len(folders))

        else:
            folders = (
                folders_cfg
                if isinstance(folders_cfg, list)
                else [folders_cfg]
            )

        for folder in folders:
            folder_dir = imap_folder_to_dir(folder)
            folder_path = account_dir / folder_dir
            state_file = folder_path / ".sync_state"
            old_state = account_dir / ".sync_state"
            # Migrate legacy flat .sync_state to inbox/.sync_state
            if rel_path == "inbox" and not state_file.exists() and old_state.exists():
                folder_path.mkdir(parents=True, exist_ok=True)
                state_file.write_text(old_state.read_text())
                logger.info("Migrated legacy .sync_state to inbox/")
            synced_uids = _load_synced_uids(state_file)

            try:
                client.select_folder(folder, readonly=True)
            except Exception:
                logger.warning("Cannot select folder '%s', skipping", folder)
                continue

            # Fetch all UIDs in folder (UIDs are per-folder in IMAP)
            all_uids: list[int] = client.search("ALL")
            new_uids = [uid for uid in all_uids if uid not in synced_uids]

            if not new_uids:
                logger.info("Folder '%s' (%s): no new messages", folder, rel_path)
                continue

            logger.info(
                "Folder '%s' (%s): %d new messages to download",
                folder,
                rel_path,
                len(new_uids),
            )

            # Download in batches of 100
            batch_size = 100
            for i in range(0, len(new_uids), batch_size):
                batch = new_uids[i : i + batch_size]
                fetched = client.fetch(batch, ["RFC822"])

                for uid, data in fetched.items():
                    raw_msg: bytes = data[b"RFC822"]
                    msg_date = _parse_date(raw_msg)
                    checksum = content_checksum(raw_msg)
                    filename = f"{checksum}-{uid}.eml"
                    filepath = folder_path / filename

                    folder_path.mkdir(parents=True, exist_ok=True)
                    filepath.write_bytes(raw_msg)
                    set_file_received_time(filepath, msg_date)
                    synced_uids.add(uid)
                    total_new += 1

                # Save state after each batch (per-folder)
                _save_synced_uids(state_file, synced_uids)

            _save_synced_uids(state_file, synced_uids)
    logger.info("Account '%s': downloaded %d new messages", account_name, total_new)
    return total_new
