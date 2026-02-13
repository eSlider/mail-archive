#!/usr/bin/env python3
"""
Migrate EML files to {checksum}-{external-id}.eml naming and set mtime from Date header.

Handles: emails/{account}/{folder}/*.eml
Renames to: {sha256[:16]}-{extracted_id}.eml
Sets file mtime from email Date header.
"""

from __future__ import annotations

import argparse
import re
from pathlib import Path

from eml_utils import content_checksum, parse_date_from_bytes, set_file_received_time

# Old format: 2021-07-14_17_28_40089_subject or 14328_subject
DATE_PREFIX = re.compile(r"^\d{4}-\d{2}-\d{2}_\d{1,2}_\d{2}_(\d+)_")


def _extract_external_id(stem: str) -> str:
    """Extract external ID from old filename. Fallback: use first segment or hash."""
    # Format: YYYY-MM-DD_HH_MM_{id}_{subject} or {id}_{subject}
    m = DATE_PREFIX.match(stem)
    if m:
        return m.group(1)
    parts = stem.split("_", 1)
    return parts[0] if parts and parts[0].isdigit() else stem[:24].replace("/", "_") or "x"


# New format: {checksum}-{id}.eml (checksum=16 hex, id=alphanumeric)
NEW_FORMAT_RE = re.compile(r"^[a-f0-9]{16}-[a-zA-Z0-9_-]+\.eml$", re.I)


def migrate_file(filepath: Path, dry_run: bool) -> bool:
    """Rename to {checksum}-{id}.eml and set mtime. Returns True if changed."""
    if filepath.suffix != ".eml":
        return False
    if filepath.name.startswith("."):
        return False
    if NEW_FORMAT_RE.match(filepath.name):
        return False  # Already in new format

    raw = filepath.read_bytes()
    checksum = content_checksum(raw)
    ext_id = _extract_external_id(filepath.stem)
    new_name = f"{checksum}-{ext_id}.eml"
    new_path = filepath.parent / new_name

    if new_path.resolve() == filepath.resolve():
        return False
    if new_path.exists():
        return False  # Skip to avoid overwrite

    received_at = parse_date_from_bytes(raw)

    if dry_run:
        print(f"Would: {filepath.name} -> {new_name} (mtime from Date header)")
        return True

    filepath.rename(new_path)
    set_file_received_time(new_path, received_at)
    return True


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Rename EML files to {checksum}-{id}.eml and set mtime from Date header"
    )
    parser.add_argument(
        "--emails-dir",
        type=Path,
        default=Path(__file__).resolve().parent.parent / "emails",
        help="Base emails directory",
    )
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()

    base = args.emails_dir
    if not base.exists():
        print(f"Error: {base} does not exist")
        return 1

    total = 0
    for account_dir in sorted(base.iterdir()):
        if not account_dir.is_dir() or account_dir.name.startswith("."):
            continue
        for filepath in account_dir.rglob("*.eml"):
            if migrate_file(filepath, args.dry_run):
                total += 1

    print(f"Total: {total} files migrated")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
