#!/usr/bin/env python3
"""
Migrate emails from date subdirectories to flat structure.

Old: emails/{account}/{YYYY-MM-DD}/{id}_{subject}.eml
New: emails/{account}/{YYYY-MM-DD}_{HH}_{MM}_{id}_{subject}.eml

Run from project root or set EMAILS_DIR.
"""

from __future__ import annotations

import argparse
import email
import email.utils
import logging
import re
from datetime import datetime, timezone
from pathlib import Path

logging.basicConfig(level=logging.INFO, format="%(levelname)s: %(message)s")
logger = logging.getLogger(__name__)

DATE_DIR_PATTERN = re.compile(r"^\d{4}-\d{2}-\d{2}$")


def _parse_date_from_eml(filepath: Path) -> datetime:
    """Extract date from email headers, fallback to file mtime."""
    try:
        raw = filepath.read_bytes()
        msg = email.message_from_bytes(raw)
        date_str = msg.get("Date", "")
        if date_str:
            parsed = email.utils.parsedate_to_datetime(date_str)
            return parsed.astimezone(timezone.utc)
    except Exception as e:
        logger.debug("Could not parse Date from %s: %s", filepath.name, e)

    # Fallback: use folder name for date, file mtime for time
    mtime = datetime.fromtimestamp(filepath.stat().st_mtime, tz=timezone.utc)
    return mtime


def _migrate_file(filepath: Path, account_dir: Path, dry_run: bool) -> bool:
    """
    Migrate a single .eml file from date subdir to flat structure.

    Returns True if migrated (or would be in dry_run), False if skipped.
    """
    if filepath.suffix != ".eml":
        return False

    date_dir = filepath.parent
    date_dir_name = date_dir.name
    if not DATE_DIR_PATTERN.match(date_dir_name):
        return False

    # Old filename: {id}_{subject}.eml
    stem = filepath.stem
    parts = stem.split("_", 1)
    id_part = parts[0]
    slug_part = parts[1] if len(parts) > 1 else "no_subject"

    msg_date = _parse_date_from_eml(filepath)
    date_prefix = msg_date.strftime("%Y-%m-%d_%H_%M")
    new_filename = f"{date_prefix}_{id_part}_{slug_part}.eml"
    new_filepath = account_dir / new_filename

    if new_filepath.exists() and new_filepath.resolve() != filepath.resolve():
        logger.warning("Target exists, skipping: %s", new_filepath.name)
        return False

    if dry_run:
        logger.info("Would move: %s -> %s", filepath.relative_to(account_dir.parent), new_filename)
        return True

    account_dir.mkdir(parents=True, exist_ok=True)
    filepath.rename(new_filepath)
    logger.info("Moved: %s -> %s", filepath.name, new_filename)
    return True


def _remove_empty_dirs(path: Path) -> None:
    """Remove directory if empty, then recurse to parent."""
    if not path.exists() or not path.is_dir():
        return
    if any(path.iterdir()):
        return
    path.rmdir()
    logger.debug("Removed empty dir: %s", path)
    _remove_empty_dirs(path.parent)


def migrate_account(account_dir: Path, dry_run: bool) -> int:
    """Migrate all emails in an account. Returns count of migrated files."""
    count = 0
    for date_dir in sorted(account_dir.iterdir()):
        if not date_dir.is_dir() or not DATE_DIR_PATTERN.match(date_dir.name):
            continue

        for filepath in list(date_dir.iterdir()):
            if _migrate_file(filepath, account_dir, dry_run):
                count += 1

        if not dry_run and date_dir.exists():
            _remove_empty_dirs(date_dir)

    return count


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Migrate emails from date subdirs to flat structure in account dir"
    )
    parser.add_argument(
        "--emails-dir",
        type=Path,
        default=Path(__file__).resolve().parent.parent / "emails",
        help="Base emails directory (default: project emails/)",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Show what would be done without moving files",
    )
    args = parser.parse_args()

    base = args.emails_dir
    if not base.exists():
        logger.error("Emails directory does not exist: %s", base)
        return 1

    total = 0
    for account_dir in sorted(base.iterdir()):
        if not account_dir.is_dir():
            continue
        if account_dir.name.startswith("."):
            continue

        n = migrate_account(account_dir, args.dry_run)
        total += n
        if n > 0:
            logger.info("Account '%s': %d files migrated", account_dir.name, n)

    if total == 0:
        logger.info("No files to migrate (structure may already be flat)")
    else:
        logger.info("Total: %d files migrated", total)

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
