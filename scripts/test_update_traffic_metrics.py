from __future__ import annotations

import copy
import unittest
from datetime import datetime, timedelta, timezone

from update_traffic_metrics import merge_metrics


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


if __name__ == "__main__":
    unittest.main()
