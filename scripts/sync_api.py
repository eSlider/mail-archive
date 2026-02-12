"""Tiny HTTP API for triggering email sync on demand.

Endpoints:
    GET  /accounts  — list configured accounts with sync status
    POST /sync      — trigger sync (all accounts or a specific one)

Started as a daemon thread from daemon.py so it doesn't block the scheduler.
"""

from __future__ import annotations

import json
import logging
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any, Callable

logger = logging.getLogger(__name__)


def start_sync_api(
    accounts: list[dict[str, Any]],
    sync_fn: Callable[[dict[str, Any]], int],
    port: int = 8081,
) -> None:
    """Start the sync trigger API in a background daemon thread."""
    state = _SyncState(accounts, sync_fn)
    handler_class = _make_handler(state)
    server = ThreadingHTTPServer(("0.0.0.0", port), handler_class)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    logger.info("Sync trigger API listening on :%d", port)


class _SyncState:
    """Thread-safe state tracker for sync operations."""

    def __init__(
        self,
        accounts: list[dict[str, Any]],
        sync_fn: Callable[[dict[str, Any]], int],
    ) -> None:
        self.accounts: dict[str, dict[str, Any]] = {
            cfg["_account_name"]: cfg for cfg in accounts
        }
        self.sync_fn = sync_fn
        self._lock = threading.Lock()
        self._status: dict[str, dict[str, Any]] = {
            name: {
                "syncing": False,
                "last_sync": None,
                "last_error": None,
                "new_messages": 0,
            }
            for name in self.accounts
        }

    def get_accounts(self) -> list[dict[str, Any]]:
        """Return account list with current sync status."""
        with self._lock:
            result = []
            for name, cfg in self.accounts.items():
                s = self._status[name].copy()
                s["name"] = name
                s["type"] = cfg["type"]
                result.append(s)
            return result

    def trigger_sync(self, account_name: str | None = None) -> tuple[int, dict]:
        """Start sync in a background thread. Returns (http_status, response_body)."""
        if account_name:
            if account_name not in self.accounts:
                return 404, {"error": f"unknown account: {account_name}"}
            targets = [account_name]
        else:
            targets = list(self.accounts.keys())

        with self._lock:
            already = [n for n in targets if self._status[n]["syncing"]]
            if already:
                return 409, {"error": "sync already running", "accounts": already}
            for n in targets:
                self._status[n]["syncing"] = True
                self._status[n]["last_error"] = None

        threading.Thread(target=self._run_sync, args=(targets,), daemon=True).start()
        return 202, {"status": "started", "accounts": targets}

    def _run_sync(self, targets: list[str]) -> None:
        for name in targets:
            cfg = self.accounts[name]
            try:
                count = self.sync_fn(cfg)
                with self._lock:
                    self._status[name]["syncing"] = False
                    self._status[name]["last_sync"] = time.time()
                    self._status[name]["last_error"] = None
                    self._status[name]["new_messages"] = count if isinstance(count, int) else 0
                logger.info("Sync API: %s finished, %s new messages", name, count)
            except Exception as exc:
                logger.exception("Sync API: %s failed", name)
                with self._lock:
                    self._status[name]["syncing"] = False
                    self._status[name]["last_error"] = str(exc)


def _make_handler(state: _SyncState) -> type:
    """Create a request handler class bound to the given state."""

    class Handler(BaseHTTPRequestHandler):
        def do_GET(self) -> None:
            if self.path == "/accounts":
                self._json(200, state.get_accounts())
            else:
                self._json(404, {"error": "not found"})

        def do_POST(self) -> None:
            if self.path == "/sync":
                length = int(self.headers.get("Content-Length", 0))
                body = json.loads(self.rfile.read(length)) if length else {}
                account = body.get("account")
                status, resp = state.trigger_sync(account)
                self._json(status, resp)
            else:
                self._json(404, {"error": "not found"})

        def _json(self, status: int, data: Any) -> None:
            payload = json.dumps(data).encode()
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Access-Control-Allow-Origin", "*")
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)

        def log_message(self, fmt: str, *args: Any) -> None:
            # Suppress default stderr logging; use our logger instead.
            logger.debug("sync-api: " + fmt, *args)

    return Handler
