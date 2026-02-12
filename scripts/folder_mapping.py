"""Map IMAP/Gmail folder names to filesystem directory names."""

from __future__ import annotations

import re

# IMAP folder names (case-insensitive) -> our dir name
IMAP_FOLDER_MAP: dict[str, str] = {
    "inbox": "inbox",
    "[gmail]/sent mail": "sent",
    "[gmail]/sent": "sent",
    "[google mail]/sent mail": "sent",
    "sent": "sent",
    "sent items": "sent",
    "sent messages": "sent",
    "[gmail]/drafts": "draft",
    "[gmail]/draft": "draft",
    "[google mail]/drafts": "draft",
    "drafts": "draft",
    "draft": "draft",
    "[gmail]/trash": "trash",
    "[google mail]/trash": "trash",
    "trash": "trash",
    "deleted": "trash",
    "[gmail]/spam": "spam",
    "[google mail]/spam": "spam",
    "spam": "spam",
    "junk": "spam",
    "[gmail]/all mail": "allmail",
    "[gmail]/alle nachrichten": "allmail",
    "[google mail]/all mail": "allmail",
    "[gmail]/gesendet": "sent",
    "[gmail]/entwürfe": "draft",
    "[gmail]/papierkorb": "trash",
    "outbox": "outbox",
}


def imap_folder_to_dir(folder_name: str) -> str:
    """Map IMAP folder name to our subdirectory name."""
    key = folder_name.strip().lower()
    return IMAP_FOLDER_MAP.get(key, _slugify_folder(key))


def _slugify_folder(name: str) -> str:
    """Convert folder name to a safe dir name (fallback for unknown folders)."""
    # Remove [brackets], replace / and \ with -
    name = re.sub(r"\[|\]", "", name)
    name = re.sub(r"[/\\]", "-", name)
    name = name.strip().lower()
    name = re.sub(r"[^\w\s-]", "", name)
    name = re.sub(r"[\s_]+", "_", name)
    return name[:40] or "other"
