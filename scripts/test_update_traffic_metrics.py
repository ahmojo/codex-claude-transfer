from __future__ import annotations

import copy
import io
import json
import unittest
from datetime import datetime, timedelta, timezone
from urllib.parse import parse_qs, urlparse

from update_traffic_metrics import fetch_release_downloads, merge_metrics


NOW = datetime(2026, 8, 7, 3, 17, tzinfo=timezone.utc)


def payload(count, uniques, *days):
    return {
        "count": count,
        "uniques": uniques,
        "clones": [
            {"timestamp": f"{day}T00:00:00Z", "count": clones, "uniques": unique}
            for day, clones, unique in days
        ],
    }


class MergeTrafficMetricsTests(unittest.TestCase):
    def test_overlapping_windows_are_idempotent(self):
        first = merge_metrics(
            None,
            payload(
                5,
                3,
                ("2026-08-05", 2, 2),
                ("2026-08-06", 3, 1),
            ),
            updated_at=NOW,
        )
        second = merge_metrics(
            first,
            payload(
                5,
                3,
                ("2026-08-05", 2, 2),
                ("2026-08-06", 3, 1),
            ),
            updated_at=NOW + timedelta(hours=1),
        )
        self.assertEqual(second, first)

    def test_existing_day_is_replaced_not_added_twice(self):
        first = merge_metrics(
            None,
            payload(2, 1, ("2026-08-05", 2, 1)),
            updated_at=NOW,
        )
        second = merge_metrics(
            first,
            payload(4, 2, ("2026-08-05", 4, 2)),
            updated_at=NOW,
        )
        self.assertEqual(second["days"]["2026-08-05"]["clones"], 4)
        self.assertEqual(second["tracked_total_clones"], 4)

    def test_total_is_sum_of_all_stored_daily_clone_counts(self):
        first = merge_metrics(
            None,
            payload(3, 2, ("2026-07-20", 3, 2)),
            updated_at=NOW,
        )
        second = merge_metrics(
            first,
            payload(
                7,
                4,
                ("2026-08-05", 2, 1),
                ("2026-08-06", 5, 3),
            ),
            updated_at=NOW,
        )
        self.assertEqual(second["tracked_since"], "2026-07-20")
        self.assertEqual(second["tracked_total_clones"], 10)
        self.assertEqual(list(second["days"]), sorted(second["days"]))

    def test_top_level_window_values_come_directly_from_api(self):
        result = merge_metrics(
            None,
            payload(99, 42, ("2026-08-06", 3, 2)),
            updated_at=NOW,
        )
        self.assertEqual(result["clones_14d"], 99)
        self.assertEqual(result["unique_cloners_14d"], 42)

    def test_does_not_calculate_all_time_unique_cloners(self):
        result = merge_metrics(
            None,
            payload(
                8,
                5,
                ("2026-08-05", 4, 3),
                ("2026-08-06", 4, 3),
            ),
            updated_at=NOW,
        )
        self.assertNotIn("tracked_total_unique_cloners", result)
        self.assertNotIn("all_time_unique_cloners", result)
        self.assertEqual(result["unique_cloners_14d"], 5)

    def test_same_api_date_uses_latest_value(self):
        result = merge_metrics(
            None,
            payload(
                4,
                2,
                ("2026-08-06", 2, 1),
                ("2026-08-06", 4, 2),
            ),
            updated_at=NOW,
        )
        self.assertEqual(result["days"]["2026-08-06"]["clones"], 4)
        self.assertEqual(result["tracked_total_clones"], 4)

    def test_invalid_api_payload_does_not_replace_existing_metrics(self):
        existing = merge_metrics(
            None,
            payload(2, 1, ("2026-08-05", 2, 1)),
            updated_at=NOW,
        )
        snapshot = copy.deepcopy(existing)

        with self.assertRaises(ValueError):
            merge_metrics(
                existing,
                {"count": 0, "uniques": 0, "clones": "invalid"},
                updated_at=NOW,
            )

        self.assertEqual(existing, snapshot)

    def test_daily_snapshots_overwrite_the_same_date(self):
        first = merge_metrics(
            None,
            payload(2, 1, ("2026-08-06", 2, 1)),
            release_downloads=10,
            updated_at=NOW,
        )
        second = merge_metrics(
            first,
            payload(4, 2, ("2026-08-06", 4, 2)),
            release_downloads=13,
            updated_at=NOW + timedelta(hours=1),
        )

        self.assertEqual(list(second["snapshots"]), ["2026-08-07"])
        self.assertEqual(
            second["snapshots"]["2026-08-07"],
            {
                "release_downloads": 13,
                "clones_14d": 4,
                "unique_cloners_14d": 2,
                "tracked_total_clones": 4,
            },
        )

    def test_failed_release_refresh_preserves_known_download_total(self):
        first = merge_metrics(
            None,
            payload(2, 1, ("2026-08-06", 2, 1)),
            release_downloads=10,
            updated_at=NOW,
        )
        second = merge_metrics(
            first,
            payload(2, 1, ("2026-08-06", 2, 1)),
            release_downloads=None,
            updated_at=NOW + timedelta(hours=1),
        )

        self.assertEqual(second["release_downloads"], 10)
        self.assertEqual(
            second["snapshots"]["2026-08-07"]["release_downloads"], 10
        )

    def test_snapshots_never_add_unique_sources_across_days(self):
        result = merge_metrics(
            None,
            payload(8, 5, ("2026-08-06", 8, 5)),
            release_downloads=20,
            updated_at=NOW,
        )

        self.assertNotIn("tracked_total_unique_cloners", result)
        self.assertNotIn("all_time_unique_cloners", result)
        self.assertEqual(
            result["snapshots"]["2026-08-07"]["unique_cloners_14d"], 5
        )


def release(*assets, draft=False):
    return {
        "draft": draft,
        "assets": [
            {"name": name, "download_count": count} for name, count in assets
        ],
    }


class ReleaseDownloadSnapshotTests(unittest.TestCase):
    @staticmethod
    def opener_for_pages(pages, requested_pages=None):
        def open_url(request, timeout):
            page = int(parse_qs(urlparse(request.full_url).query)["page"][0])
            if requested_pages is not None:
                requested_pages.append(page)
            body = json.dumps(pages.get(page, [])).encode("utf-8")
            return io.BytesIO(body)

        return open_url

    def test_counts_only_current_and_historical_platform_binaries(self):
        pages = {
            1: [
                release(
                    ("cct_v1.8.0_windows_amd64.tar.gz", 4),
                    ("cct_v1.8.0_darwin_arm64.tar.gz", 7),
                    ("codex-sync_v0.1.13_linux_amd64.tar.gz", 11),
                    ("SHA256SUMS.txt", 100),
                    ("SHA256SUMS.txt.sigstore.json", 100),
                    ("cct_v1.8.0_sbom.spdx.json", 100),
                    ("codex-claude-transfer-1.8.0-deps.tar.xz", 100),
                ),
                release(("cct_v1.9.0_linux_amd64.tar.gz", 50), draft=True),
            ]
        }

        self.assertEqual(
            fetch_release_downloads(self.opener_for_pages(pages)), 22
        )

    def test_release_fetch_paginates_after_100_releases(self):
        requested_pages = []
        pages = {
            1: [release()] * 100,
            2: [release(("cct_v2.0.0_linux_arm64.zip", 9))],
        }

        self.assertEqual(
            fetch_release_downloads(
                self.opener_for_pages(pages, requested_pages)
            ),
            9,
        )
        self.assertEqual(requested_pages, [1, 2])


if __name__ == "__main__":
    unittest.main()
