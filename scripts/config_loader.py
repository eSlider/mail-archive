"""Load and validate account YAML configs from config/ directory."""

from __future__ import annotations

import logging
import re
from pathlib import Path
from typing import Any

import yaml

logger = logging.getLogger(__name__)

VALID_TYPES = {"IMAP", "POP3", "GMAIL_API"}


def parse_interval(interval_str: str) -> int:
    """
    Parse human-readable interval string to seconds.

    Supports: 30s, 5m, 1h, 1d or combinations like '1h30m'.
    """
    total = 0
    units = {"s": 1, "m": 60, "h": 3600, "d": 86400}
    for match in re.finditer(r"(\d+)\s*([smhd])", interval_str.lower()):
        value = int(match.group(1))
        unit = match.group(2)
        total += value * units[unit]
    return total if total > 0 else 300  # Default: 5 minutes


def load_account_config(filepath: Path) -> dict[str, Any] | None:
    """Load and validate a single account YAML config."""
    try:
        with open(filepath) as f:
            cfg = yaml.safe_load(f)
    except Exception:
        logger.exception("Failed to parse config %s", filepath)
        return None

    if not isinstance(cfg, dict):
        logger.error("Config %s is not a valid YAML mapping", filepath)
        return None

    account_type = cfg.get("type", "").upper()
    if account_type not in VALID_TYPES:
        logger.error(
            "Config %s: unknown type '%s' (expected one of %s)",
            filepath,
            account_type,
            VALID_TYPES,
        )
        return None

    cfg["type"] = account_type

    if "email" not in cfg:
        logger.error("Config %s: missing 'email' field", filepath)
        return None

    # Parse sync interval
    sync_cfg = cfg.get("sync", {})
    interval_str = sync_cfg.get("interval", "5m")
    cfg["_interval_seconds"] = parse_interval(interval_str)

    # Derive account name from filename
    cfg["_account_name"] = filepath.stem

    return cfg


def load_all_configs(config_dir: Path) -> list[dict[str, Any]]:
    """Load all *.yml configs from the config directory."""
    configs: list[dict[str, Any]] = []

    if not config_dir.is_dir():
        logger.error("Config directory %s does not exist", config_dir)
        return configs

    for filepath in sorted(config_dir.glob("*.yml")):
        if filepath.name == "example.yml":
            continue
        cfg = load_account_config(filepath)
        if cfg is not None:
            configs.append(cfg)
            logger.info("Loaded config for '%s' (%s)", cfg["_account_name"], cfg["type"])

    return configs
