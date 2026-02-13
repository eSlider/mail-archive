#!/usr/bin/env python3
"""
Migrate from flat folder structure to hierarchical path structure.

Old: emails/{account}/{flat-slug}/file.eml
     e.g. emails/eslider-gmail/gmail-benachrichtigung-kartinatv/0f562bfad5b8b879-2.eml

New: emails/{account}/{path}/file.eml
     e.g. emails/eslider-gmail/gmail/benachrichtigung/kartinatv/0f562bfad5b8b879-2.eml

Conversion: flat-slug "gmail-benachrichtigung-kartinatv" -> "gmail/benachrichtigung/kartinatv"
(separator "-" becomes path separator "/")
"""

from __future__ import annotations

import argparse
from pathlib import Path

from folder_mapping import old_slug_to_path


def migrate_account(account_dir: Path, dry_run: bool) -> int:
    """Migrate one account's folder structure. Returns count of migrated files."""
    count = 0

    for item in sorted(account_dir.iterdir()):
        if not item.is_dir():
            continue
        if item.name.startswith("."):
            continue

        old_slug = item.name
        new_rel = old_slug_to_path(old_slug)
        new_path = account_dir / new_rel

        if new_path.resolve() == item.resolve():
            continue  # Already correct

        for sub in list(item.iterdir()):
            target = new_path / sub.name
            if target.exists():
                continue  # Skip existing

            if dry_run:
                print(f"Would move: {item.name}/{sub.name} -> {new_rel}/{sub.name}")
            else:
                new_path.mkdir(parents=True, exist_ok=True)
                sub.rename(target)
            count += 1

        if not dry_run and item.exists() and not any(item.iterdir()):
            item.rmdir()

    return count


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Migrate flat folder structure to hierarchical path structure"
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
        n = migrate_account(account_dir, args.dry_run)
        total += n
        if n > 0:
            print(f"Account '{account_dir.name}': {n} files migrated")

    print(f"Total: {total} files migrated")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
