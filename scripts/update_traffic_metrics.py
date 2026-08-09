#!/usr/bin/env python3
"""Merge GitHub's overlapping 14-day clone windows into a stable history."""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
import tempfile
from datetime import date, datetime, timezone
from pathlib import Path
from typing import Any, Callable
from urllib.request import Request, urlopen

REPO = "ahmojo/codex-claude-transfer"
SCHEMA_VERSION = 1
RELEASES_URL = f"https://api.github.com/repos/{REPO}/releases"
SNAPSHOT_FIELDS = (
    "release_downloads",
    "clones_14d",
    "unique_cloners_14d",
    "tracked_total_clones",
)
BINARY_ASSET_RE = re.compile(
    r"^(?:cct|codex-sync)_v[^_]+_(?:linux|darwin|windows)_"
    r"(?:amd64|arm64)\.(?:tar\.gz|zip)$",
    re.IGNORECASE,
)


class MetricsError(ValueError):
    """Raised when an input document cannot be trusted."""


def non_negative_int(value: Any, field: str) -> int:
    if not isinstance(value, int) or isinstance(value, bool) or value < 0:
        raise MetricsError(f"{field} must be a non-negative integer")
    return value


def timestamp_day(value: Any) -> str:
    if not isinstance(value, str):
        raise MetricsError("clone timestamp must be a string")
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError as exc:
        raise MetricsError(f"invalid clone timestamp: {value}") from exc
    if parsed.tzinfo is None:
        raise MetricsError("clone timestamp must include a timezone")
    return parsed.astimezone(timezone.utc).date().isoformat()


def validate_day_key(day_key: Any, field: str) -> str:
    try:
        normalized_day = date.fromisoformat(day_key).isoformat()
    except (TypeError, ValueError) as exc:
        raise MetricsError(f"invalid {field}: {day_key}") from exc
    if normalized_day != day_key:
        raise MetricsError(f"invalid {field}: {day_key}")
    return normalized_day


def validate_existing(
    existing: dict[str, Any] | None,
) -> tuple[dict[str, dict[str, int]], dict[str, dict[str, int]]]:
    if existing is None:
        return {}, {}
    if not isinstance(existing, dict):
        raise MetricsError("existing metrics must be a JSON object")
    if existing.get("schema_version") != SCHEMA_VERSION:
        raise MetricsError("existing metrics use an unsupported schema")
    if existing.get("repo") != REPO:
        raise MetricsError("existing metrics belong to a different repository")
    raw_days = existing.get("days")
    if not isinstance(raw_days, dict):
        raise MetricsError("existing metrics days must be an object")

    days: dict[str, dict[str, int]] = {}
    for day_key, values in raw_days.items():
        validate_day_key(day_key, "stored day")
        if not isinstance(values, dict):
            raise MetricsError(f"invalid stored metrics for {day_key}")
        days[day_key] = {
            "clones": non_negative_int(values.get("clones"), f"{day_key}.clones"),
            "unique_cloners": non_negative_int(
                values.get("unique_cloners"), f"{day_key}.unique_cloners"
            ),
        }
    raw_snapshots = existing.get("snapshots", {})
    if not isinstance(raw_snapshots, dict):
        raise MetricsError("existing metrics snapshots must be an object")
    snapshots: dict[str, dict[str, int]] = {}
    for day_key, values in raw_snapshots.items():
        validate_day_key(day_key, "snapshot day")
        if not isinstance(values, dict):
            raise MetricsError(f"invalid snapshot metrics for {day_key}")
        snapshot: dict[str, int] = {}
        for field in SNAPSHOT_FIELDS:
            if field in values:
                snapshot[field] = non_negative_int(
                    values[field], f"snapshots.{day_key}.{field}"
                )
        if not snapshot:
            raise MetricsError(f"snapshot {day_key} has no supported metrics")
        snapshots[day_key] = snapshot
    return days, snapshots


def fetch_release_downloads(
    open_url: Callable[..., Any] = urlopen,
) -> int:
    """Count installable binaries across every published GitHub release."""
    total = 0
    page = 1
    while True:
        request = Request(
            f"{RELEASES_URL}?per_page=100&page={page}",
            headers={
                "Accept": "application/vnd.github+json",
                "User-Agent": "cct-traffic-metrics/1.0",
                "X-GitHub-Api-Version": "2022-11-28",
            },
        )
        with open_url(request, timeout=15) as response:
            releases = json.load(response)
        if not isinstance(releases, list):
            raise MetricsError("GitHub releases response must be a JSON array")
        for release in releases:
            if not isinstance(release, dict) or release.get("draft"):
                continue
            assets = release.get("assets", [])
            if not isinstance(assets, list):
                continue
            for asset in assets:
                if not isinstance(asset, dict):
                    continue
                if BINARY_ASSET_RE.fullmatch(str(asset.get("name", ""))):
                    total += non_negative_int(
                        asset.get("download_count"), "asset.download_count"
                    )
        if len(releases) < 100:
            return total
        page += 1


def merge_metrics(
    existing: dict[str, Any] | None,
    api_payload: dict[str, Any],
    *,
    release_downloads: int | None = None,
    updated_at: datetime | None = None,
) -> dict[str, Any]:
    """Replace overlapping days and recompute the cumulative clone count."""
    if not isinstance(api_payload, dict):
        raise MetricsError("GitHub traffic response must be a JSON object")
    clones_14d = non_negative_int(api_payload.get("count"), "count")
    unique_cloners_14d = non_negative_int(api_payload.get("uniques"), "uniques")
    api_days = api_payload.get("clones")
    if not isinstance(api_days, list):
        raise MetricsError("clones must be a list")

    if release_downloads is not None:
        release_downloads = non_negative_int(
            release_downloads, "release_downloads"
        )
    days, snapshots = validate_existing(existing)
    for entry in api_days:
        if not isinstance(entry, dict):
            raise MetricsError("each clone day must be an object")
        day_key = timestamp_day(entry.get("timestamp"))
        days[day_key] = {
            "clones": non_negative_int(entry.get("count"), f"{day_key}.count"),
            "unique_cloners": non_negative_int(
                entry.get("uniques"), f"{day_key}.uniques"
            ),
        }

    now = updated_at or datetime.now(timezone.utc)
    if now.tzinfo is None:
        raise MetricsError("updated_at must include a timezone")
    now = now.astimezone(timezone.utc)
    sorted_days = dict(sorted(days.items()))
    tracked_since = min(sorted_days) if sorted_days else now.date().isoformat()
    tracked_total_clones = sum(
        day["clones"] for day in sorted_days.values()
    )
    snapshot_day = now.date().isoformat()
    snapshot = {
        "clones_14d": clones_14d,
        "unique_cloners_14d": unique_cloners_14d,
        "tracked_total_clones": tracked_total_clones,
    }
    if release_downloads is not None:
        snapshot["release_downloads"] = release_downloads
    elif "release_downloads" in snapshots.get(snapshot_day, {}):
        snapshot["release_downloads"] = snapshots[snapshot_day][
            "release_downloads"
        ]
    snapshots[snapshot_day] = snapshot
    sorted_snapshots = dict(sorted(snapshots.items()))

    result = {
        "schema_version": SCHEMA_VERSION,
        "repo": REPO,
        "tracked_since": tracked_since,
        "updated_at": now.isoformat(timespec="seconds").replace("+00:00", "Z"),
        "clones_14d": clones_14d,
        "unique_cloners_14d": unique_cloners_14d,
        "tracked_total_clones": tracked_total_clones,
        "days": sorted_days,
        "snapshots": sorted_snapshots,
    }
    if release_downloads is not None:
        result["release_downloads"] = release_downloads
    elif isinstance(existing, dict) and isinstance(
        existing.get("release_downloads"), int
    ):
        result["release_downloads"] = non_negative_int(
            existing["release_downloads"], "release_downloads"
        )
    if isinstance(existing, dict):
        previous_semantics = {
            key: value for key, value in existing.items() if key != "updated_at"
        }
        current_semantics = {
            key: value for key, value in result.items() if key != "updated_at"
        }
        if previous_semantics == current_semantics and isinstance(
            existing.get("updated_at"), str
        ):
            result["updated_at"] = existing["updated_at"]
    return result


def read_json(path: Path, *, required: bool) -> dict[str, Any] | None:
    if not path.exists():
        if required:
            raise MetricsError(f"missing input file: {path}")
        return None
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise MetricsError(f"cannot read valid JSON from {path}") from exc
    if not isinstance(data, dict):
        raise MetricsError(f"{path} must contain a JSON object")
    return data


def write_json_atomic(path: Path, data: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    rendered = json.dumps(data, indent=2, ensure_ascii=False) + "\n"
    descriptor, temp_name = tempfile.mkstemp(
        prefix=f".{path.name}.", suffix=".tmp", dir=path.parent
    )
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8", newline="\n") as handle:
            handle.write(rendered)
        os.replace(temp_name, path)
    except Exception:
        try:
            os.unlink(temp_name)
        except FileNotFoundError:
            pass
        raise


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--input", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    try:
        release_downloads = fetch_release_downloads()
    except Exception as exc:
        release_downloads = None
        print(
            f"release download snapshot unavailable: {exc}",
            file=sys.stderr,
        )
    merged = merge_metrics(
        read_json(args.output, required=False),
        read_json(args.input, required=True) or {},
        release_downloads=release_downloads,
    )
    write_json_atomic(args.output, merged)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
