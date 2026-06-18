#!/usr/bin/env python3
"""Generate fake Codex homes for the cct demo recordings.

This NEVER touches a real ~/.codex. It writes self-contained homes under a
chosen base directory, with realistic-looking sessions across a few projects so
the grouped `cct list` and the `--merge` incremental-sync workflow can be shown
on camera without exposing any real prompts, code, or secrets.

Usage: python3 gen_demo_home.py <base-dir>
Creates:
  <base>/laptop/codex-home   full, up-to-date sessions (the "source" device)
  <base>/pc/codex-home       same projects, but one session is shorter and one
                             newest session is missing (the "target" device), so
                             `cct import --merge` shows "1 new" + "1 updated".
"""
import json
import os
import sys
import time
from datetime import datetime, timezone

# Demo projects live under ~/projects so the recorder can create the folders,
# making imported sessions show as [ok] (not [missing]) for clean on-camera output.
# CCT_DEMO_BASE overrides the project-path prefix (e.g. a Windows path for the
# WebUI recording); it only changes the recorded cwd strings, nothing else.
BASE = os.environ.get("CCT_DEMO_BASE", os.path.expanduser("~/projects"))
SEP = "\\" if "\\" in BASE else "/"  # match the base's path style (Win vs POSIX)

# (project cwd, [ (uuid, "YYYY-MM-DDTHH:MM", preview-message, extra_turns) ])
# extra_turns are additional user/assistant lines appended to simulate a session
# that grew on the laptop after the pc last saw it.
PROJECTS = [
    (BASE + SEP + "todo-api", [
        ("a1b2c3d4", "2026-06-08T10:15", "Sketch out the REST endpoints before we start coding.", 0),
        ("b2c3d4e5", "2026-06-10T14:30", "Add JWT auth to the login and refresh endpoints.", 3),
        ("c3d4e5f6", "2026-06-12T09:05", "Move shared request validation into one middleware.", 0),
        ("d4e5f6a7", "2026-06-14T16:45", "Add pagination to the list-items endpoint.", 0),
    ]),
    (BASE + SEP + "weather-cli", [
        ("e5f6a7b8", "2026-06-09T11:20", "Set up the CLI scaffolding and a --units flag.", 0),
        ("f6a7b8c9", "2026-06-13T13:50", "Cache the forecast responses for 10 minutes.", 0),
    ]),
    (BASE + SEP + "recipe-site", [
        ("a7b8c9d0", "2026-06-11T08:40", "Write the importer that loads recipes from JSON.", 0),
        ("b8c9d0e1", "2026-06-15T17:10", "Add retry logic when the upstream API times out.", 0),
    ]),
]


def iso(ts: str, secs: int = 0) -> str:
    dt = datetime.strptime(ts, "%Y-%m-%dT%H:%M").replace(tzinfo=timezone.utc)
    return dt.strftime("%Y-%m-%dT%H:%M:") + f"{secs:02d}Z"


def meta_line(uuid: str, ts: str, cwd: str) -> str:
    return json.dumps({
        "timestamp": iso(ts),
        "type": "session_meta",
        "payload": {
            "id": uuid,
            "timestamp": iso(ts),
            "cwd": cwd,
            "originator": "vscode",
            "cli_version": "0.21.0",
            "source": "cli",
            "model_provider": "openai",
        },
    })


def user_line(ts: str, secs: int, msg: str) -> str:
    return json.dumps({
        "timestamp": iso(ts, secs),
        "type": "event_msg",
        "payload": {"type": "user_message", "message": msg},
    })


def assistant_line(ts: str, secs: int, msg: str) -> str:
    return json.dumps({
        "timestamp": iso(ts, secs),
        "type": "response_item",
        "payload": {"type": "message", "role": "assistant",
                    "content": [{"type": "output_text", "text": msg}]},
    })


def write_session(home: str, uuid: str, ts: str, cwd: str, msg: str, extra: int):
    dt = datetime.strptime(ts, "%Y-%m-%dT%H:%M").replace(tzinfo=timezone.utc)
    day_dir = os.path.join(home, "sessions", dt.strftime("%Y"), dt.strftime("%m"), dt.strftime("%d"))
    os.makedirs(day_dir, exist_ok=True)
    fname = f"rollout-{dt.strftime('%Y-%m-%dT%H-%M-%S')}-{uuid}.jsonl"
    path = os.path.join(day_dir, fname)

    lines = [meta_line(uuid, ts, cwd), user_line(ts, 5, msg),
             assistant_line(ts, 9, "Sure — here's a plan and the first changes.")]
    for i in range(extra):
        lines.append(user_line(ts, 20 + i * 4, f"Looks good, also handle edge case {i + 1}."))
        lines.append(assistant_line(ts, 22 + i * 4, f"Done — handled edge case {i + 1}."))

    with open(path, "w", encoding="utf-8") as fh:
        fh.write("\n".join(lines) + "\n")

    # UpdatedAt is the file modtime, so set it to the session time for ordering.
    epoch = dt.timestamp()
    os.utime(path, (epoch, epoch))
    return path


def build(base: str, device: str, shorten_grown: bool, drop_newest: bool):
    home = os.path.join(base, device, "codex-home")
    os.makedirs(home, exist_ok=True)
    for cwd, sessions in PROJECTS:
        for uuid, ts, msg, extra in sessions:
            # On the PC the grown session is shorter (no extra turns)…
            ex = extra
            if shorten_grown and extra > 0:
                ex = 0
            # …and the newest session does not exist yet.
            if drop_newest and uuid == "b8c9d0e1":
                continue
            write_session(home, uuid, ts, cwd, msg, ex)
    return home


def main():
    if len(sys.argv) != 2:
        print("usage: gen_demo_home.py <base-dir>", file=sys.stderr)
        sys.exit(2)
    base = sys.argv[1]
    laptop = build(base, "laptop", shorten_grown=False, drop_newest=False)
    pc = build(base, "pc", shorten_grown=True, drop_newest=True)
    print("laptop home:", laptop)
    print("pc home:    ", pc)


if __name__ == "__main__":
    main()
