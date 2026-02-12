"""Gmail API sync: uses OAuth2 to download emails via the Gmail REST API."""

from __future__ import annotations

import base64
import logging
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from eml_utils import content_checksum, set_file_received_time

logger = logging.getLogger(__name__)

# Gmail label IDs -> our dir names
GMAIL_LABEL_MAP: dict[str, str] = {
    "INBOX": "inbox",
    "SENT": "sent",
    "DRAFT": "draft",
    "TRASH": "trash",
    "SPAM": "spam",
    "UNREAD": "unread",  # Usually combined with others
}


def _load_synced_ids(state_file: Path) -> set[str]:
    """Load previously synced message IDs."""
    if not state_file.exists():
        return set()
    text = state_file.read_text().strip()
    if not text:
        return set()
    return {mid for mid in text.split("\n") if mid.strip()}


def _save_synced_ids(state_file: Path, ids: set[str]) -> None:
    """Persist synced message IDs."""
    state_file.parent.mkdir(parents=True, exist_ok=True)
    state_file.write_text("\n".join(sorted(ids)))


def _get_gmail_service(account_name: str, cfg: dict[str, Any], secrets_dir: Path) -> Any:
    """Build an authenticated Gmail API service."""
    # Lazy imports so the module is loadable even without google libs installed
    from google.auth.transport.requests import Request
    from google.oauth2.credentials import Credentials
    from google_auth_oauthlib.flow import InstalledAppFlow
    from googleapiclient.discovery import build

    scopes = ["https://www.googleapis.com/auth/gmail.readonly"]
    token_path = secrets_dir / f"{account_name}_token.json"
    credentials_path = Path(cfg.get("credentials_file", str(secrets_dir / "credentials.json")))

    creds: Credentials | None = None

    if token_path.exists():
        creds = Credentials.from_authorized_user_file(str(token_path), scopes)

    if creds is None or not creds.valid:
        if creds and creds.expired and creds.refresh_token:
            creds.refresh(Request())
        else:
            if not credentials_path.exists():
                raise FileNotFoundError(
                    f"Gmail OAuth credentials not found at {credentials_path}. "
                    "Download from Google Cloud Console -> APIs & Services -> Credentials."
                )
            flow = InstalledAppFlow.from_client_secrets_file(str(credentials_path), scopes)
            creds = flow.run_local_server(port=0)

        token_path.write_text(creds.to_json())

    return build("gmail", "v1", credentials=creds)


def sync_gmail_api_account(
    account_name: str,
    cfg: dict[str, Any],
    base_dir: Path,
    secrets_dir: Path,
) -> int:
    """
    Sync emails from a Gmail account via the API.

    Returns the number of newly downloaded messages.
    """
    labels: list[str] = cfg.get("labels", ["INBOX", "SENT", "DRAFT"])
    account_dir = base_dir / account_name
    total_new = 0

    service = _get_gmail_service(account_name, cfg, secrets_dir)

    for label_id in labels:
        folder_dir = GMAIL_LABEL_MAP.get(label_id, label_id.lower().replace("-", "_"))
        folder_path = account_dir / folder_dir
        state_file = folder_path / ".sync_state"
        synced_ids = _load_synced_ids(state_file)

        # Migrate legacy .sync_state to inbox/
        old_state = account_dir / ".sync_state"
        if folder_dir == "inbox" and not state_file.exists() and old_state.exists():
            folder_path.mkdir(parents=True, exist_ok=True)
            state_file.write_text(old_state.read_text())
            synced_ids = _load_synced_ids(state_file)

        page_token: str | None = None
        all_msg_ids: list[str] = []

        while True:
            results = (
                service.users()
                .messages()
                .list(
                    userId="me",
                    labelIds=[label_id],
                    pageToken=page_token,
                    maxResults=500,
                )
                .execute()
            )
            messages = results.get("messages", [])
            all_msg_ids.extend(m["id"] for m in messages)
            page_token = results.get("nextPageToken")
            if not page_token:
                break

        new_ids = [mid for mid in all_msg_ids if mid not in synced_ids]
        if not new_ids:
            logger.info("Gmail '%s' %s: no new messages", account_name, folder_dir)
            continue

        logger.info(
            "Gmail '%s' %s: %d new messages to download",
            account_name,
            folder_dir,
            len(new_ids),
        )

        for mid in new_ids:
            msg = (
                service.users()
                .messages()
                .get(userId="me", id=mid, format="raw")
                .execute()
            )
            raw_bytes = base64.urlsafe_b64decode(msg["raw"])

            internal_date_ms = int(msg.get("internalDate", 0))
            msg_date = datetime.fromtimestamp(
                internal_date_ms / 1000, tz=timezone.utc
            )
            checksum = content_checksum(raw_bytes)
            filename = f"{checksum}-{mid}.eml"
            filepath = folder_path / filename

            folder_path.mkdir(parents=True, exist_ok=True)
            filepath.write_bytes(raw_bytes)
            set_file_received_time(filepath, msg_date)
            synced_ids.add(mid)
            total_new += 1

            if total_new % 100 == 0:
                _save_synced_ids(state_file, synced_ids)

        _save_synced_ids(state_file, synced_ids)

    logger.info("Gmail '%s': downloaded %d new messages", account_name, total_new)
    return total_new
