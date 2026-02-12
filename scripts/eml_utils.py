"""Shared utilities for EML file handling."""

from __future__ import annotations

import hashlib
import os
from datetime import datetime, timezone
from pathlib import Path


def content_checksum(raw: bytes, length: int = 16) -> str:
    """SHA-256 hash of raw EML content, truncated to `length` hex chars."""
    return hashlib.sha256(raw).hexdigest()[:length]


def set_file_received_time(filepath: Path, received_at: datetime) -> None:
    """Set file mtime/atime to the email's received timestamp."""
    try:
        ts = received_at.timestamp()
        os.utime(filepath, (ts, ts))
    except OSError:
        pass  # May fail in restricted environments (e.g. sandbox)


def _date_header_to_str(val: object) -> str:
    """Convert Date header value to string (handles Header objects)."""
    if val is None:
        return ""
    s = str(val).replace("\n", " ").replace("\r", "").strip()
    return s


def parse_date_from_bytes(raw: bytes) -> datetime:
    """Extract Date from email headers, fallback to now (UTC)."""
    import email
    import email.utils

    msg = email.message_from_bytes(raw)
    val = msg.get("Date")
    date_str = _date_header_to_str(val)
    if date_str:
        try:
            parsed = email.utils.parsedate_to_datetime(date_str)
            return parsed.astimezone(timezone.utc)
        except (TypeError, ValueError, AttributeError):
            pass
    return datetime.now(timezone.utc)
