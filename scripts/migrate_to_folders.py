#!/usr/bin/env python3
"""
Migrate flat emails into folder subdirectories (inbox, sent, draft).

Source: emails/{account}/{filename}.eml  or  emails/{account}/{date}/{filename}.eml
Target: emails/{account}/inbox/{filename}.eml  (default to inbox; can't detect folder from file)

Run from project root or set EMAILS_DIR.
"""

from __future__ import annotations

import argparse
import re
from pathlib import Path

import logging

logging.basicConfig(level=logging.INFO, format="%(levelname)s: %(message)s")
logger = logging.getLogger(__name__)

DATE_DIR_PATTERN = re.compile(r"^\d{4}-\d{2}-\d{2}$")
FOLDER_NAMES = {"inbox", "sent", "draft", "trash", "spam", "allmail", "outbox"}


def _migrate_to_inbox(filepath: Path, inbox_dir: Path, dry_run: bool) -> bool:
    """Move a single .eml file into inbox/. Returns True if moved."""
    if filepath.suffix != ".eml":
        return False

    target = inbox_dir / filepath.name
    if target.exists() and target.resolve() != filepath.resolve():
        logger.warning("Target exists, skipping: %s", filepath.name)
        return False

    if dry_run:
        logger.info("Would move: %s -> inbox/%s", filepath.name, filepath.name)
        return True

    inbox_dir.mkdir(parents=True, exist_ok=True)
    filepath.rename(target)
    logger.info("Moved: %s -> inbox/%s", filepath.name, filepath.name)
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
    """Migrate flat/datelayer emails to inbox/. Returns count."""
    count = 0
    inbox_dir = account_dir / "inbox"

    for item in sorted(account_dir.iterdir()):
        if item.name.startswith("."):
            continue
        if item.is_dir() and item.name in FOLDER_NAMES:
            continue  # Already in folder structure
        if item.is_file() and item.suffix == ".eml":
            if _migrate_to_inbox(item, inbox_dir, dry_run):
                count += 1
        elif item.is_dir() and DATE_DIR_PATTERN.match(item.name):
            for filepath in list(item.iterdir()):
                if _migrate_to_inbox(filepath, inbox_dir, dry_run):
                    count += 1
            if not dry_run and item.exists():
                _remove_empty_dirs(item)
        elif item.is_dir() and item.name not in FOLDER_NAMES:
            # Unknown subdir (e.g. old date dirs) - move .eml files into inbox
            for filepath in list(item.iterdir()):
                if _migrate_to_inbox(filepath, inbox_dir, dry_run):
                    count += 1
            if not dry_run and item.exists():
                _remove_empty_dirs(item)

    return count


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Migrate flat emails into inbox/ (and other folder subdirs)"
    )
    parser.add_argument(
        "--emails-dir",
        type=Path,
        default=Path(__file__).resolve().parent.parent / "emails",
        help="Base emails directory",
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
            logger.info("Account '%s': %d files migrated to inbox/", account_dir.name, n)

    if total == 0:
        logger.info("No files to migrate (may already use folder structure)")
    else:
        logger.info("Total: %d files migrated", total)

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
