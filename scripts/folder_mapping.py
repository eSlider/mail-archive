"""Map IMAP/Gmail folder names to hierarchical filesystem paths."""

from __future__ import annotations

import re

# IMAP folder names (case-insensitive) -> our path (relative, slash-separated)
# Single segment = flat; multiple = hierarchy
IMAP_FOLDER_MAP: dict[str, str] = {
    "inbox": "inbox",
    "[gmail]/sent mail": "gmail/sent",
    "[gmail]/sent": "gmail/sent",
    "[gmail]/gesendet": "gmail/sent",
    "[google mail]/sent mail": "gmail/sent",
    "[gmail]/drafts": "gmail/draft",
    "[gmail]/draft": "gmail/draft",
    "[gmail]/entwürfe": "gmail/draft",
    "[google mail]/drafts": "gmail/draft",
    "[gmail]/trash": "gmail/trash",
    "[gmail]/papierkorb": "gmail/trash",
    "[google mail]/trash": "gmail/trash",
    "[gmail]/spam": "gmail/spam",
    "[google mail]/spam": "gmail/spam",
    "[gmail]/all mail": "gmail/allmail",
    "[gmail]/alle nachrichten": "gmail/allmail",
    "[google mail]/all mail": "gmail/allmail",
    "[gmail]/marked": "gmail/marked",
    "[gmail]/markiert": "gmail/marked",
    "[gmail]/important": "gmail/important",
    "[gmail]/wichtig": "gmail/important",
}


def _slugify_part(name: str, max_len: int = 40) -> str:
    """Convert a single path segment to a safe dir name."""
    name = re.sub(r"\[|\]", "", name)
    name = name.strip().lower()
    name = re.sub(r"[^\w\s\-.]", "", name)
    name = re.sub(r"[.\s_\-]+", "_", name).strip("_")
    return name[:max_len] or "other"


def imap_folder_to_path(folder_name: str) -> str:
    """
    Map IMAP folder name to hierarchical path.

    [Gmail]/Benachrichtigung/Kartina.TV -> gmail/benachrichtigung/kartina_tv
    INBOX -> inbox
    """
    key = folder_name.strip().lower()
    if key in IMAP_FOLDER_MAP:
        return IMAP_FOLDER_MAP[key]

    parts = folder_name.replace("\\", "/").split("/")
    slug_parts = [_slugify_part(p) for p in parts if p.strip()]

    # [Gmail] becomes "gmail"
    if slug_parts and slug_parts[0] == "gmail":
        slug_parts[0] = "gmail"
    elif slug_parts:
        first = slug_parts[0]
        if first in ("gmail", "google_mail"):
            slug_parts[0] = "gmail"

    return "/".join(slug_parts) if slug_parts else "other"


def old_slug_to_path(old_slug: str) -> str:
    """
    Convert old flat slug to new hierarchical path.

    gmail-benachrichtigung-kartinatv -> gmail/benachrichtigung/kartinatv
    inbox -> inbox
    allmail -> gmail/allmail (was [Gmail]/All Mail)
    (old format used "-" for path sep)
    """
    if old_slug == "inbox":
        return "inbox"
    if old_slug in ("sent", "draft", "trash", "spam", "allmail"):
        return f"gmail/{old_slug}"
    parts = old_slug.split("-")
    return "/".join(p for p in parts if p)
