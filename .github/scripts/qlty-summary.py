#!/usr/bin/env python3
"""Parse qlty JSON report and write a Markdown summary to $GITHUB_STEP_SUMMARY."""

import json
import os
import sys
from collections import defaultdict

REPORT_FILE = "qlty-report.json"
SUMMARY_FILE = os.environ.get("GITHUB_STEP_SUMMARY")


def load_report(path: str):
    with open(path, "r") as f:
        return json.load(f)


def write_summary(text: str) -> None:
    if SUMMARY_FILE:
        with open(SUMMARY_FILE, "a") as f:
            f.write(text + "\n")
    else:
        print(text)


def main() -> None:
    if not os.path.exists(REPORT_FILE):
        write_summary("## qlty Quality Report\n\n⚠️ Report file not found.")
        sys.exit(0)

    try:
        report = load_report(REPORT_FILE)
    except json.JSONDecodeError as exc:
        write_summary(f"## qlty Quality Report\n\n⚠️ Could not parse report JSON: {exc}")
        sys.exit(0)

    # qlty --json outputs a bare array, not {"issues": [...]}
    issues = report if isinstance(report, list) else (report.get("issues") or report.get("results") or [])

    if not issues:
        write_summary("## qlty Quality Report\n\n✅ No issues found.")
        return

    # Tally by severity
    severity_counts: dict[str, int] = defaultdict(int)
    files_with_issues: dict[str, list] = defaultdict(list)

    for issue in issues:
        # Normalise: strip "LEVEL_" prefix, lowercase; fall back to "unknown"
        raw_level = (issue.get("level") or issue.get("severity") or "unknown")
        sev = raw_level.lower().removeprefix("level_")
        severity_counts[sev] += 1

        # Collect file path
        location = issue.get("location") or {}
        path = (
            location.get("path")
            or issue.get("path")
            or issue.get("file")
            or "unknown"
        )
        files_with_issues[path].append(issue)

    total = len(issues)

    lines = [
        "## qlty Quality Report",
        "",
        f"**{total} issue{'s' if total != 1 else ''} found**",
        "",
        "### Severity breakdown",
        "",
        "| Severity | Count |",
        "|----------|-------|",
    ]

    severity_order = ["high", "medium", "low", "unknown"]
    reported_severities = set(severity_counts.keys())
    ordered = [s for s in severity_order if s in reported_severities]
    ordered += sorted(reported_severities - set(severity_order))

    for sev in ordered:
        icon = {"high": "🔴", "medium": "🟡", "low": "🔵"}.get(sev, "⚪")
        lines.append(f"| {icon} {sev.capitalize()} | {severity_counts[sev]} |")

    lines += [
        "",
        "### Files with issues",
        "",
    ]

    for filepath in sorted(files_with_issues.keys()):
        count = len(files_with_issues[filepath])
        lines.append(f"- `{filepath}` — {count} issue{'s' if count != 1 else ''}")

    write_summary("\n".join(lines))


if __name__ == "__main__":
    main()
