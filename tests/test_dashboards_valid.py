"""Validate dashboard + library-panel JSON.

The macro dashboards (world-macro, macro-compare) use **inline `source`** targets
with the country baked in per panel (not a `$country` repeat variable — panel
repeat's scopedVars don't reach the core-datasource query, so every repeated panel
resolved to the same country). So a core-datasource target is valid if it either
names a shipped metric via `ref`, or carries a non-empty inline `@metric` source.
"""
import glob
import json
import os
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

METRICS = {
    os.path.splitext(os.path.basename(p))[0]
    for p in glob.glob(str(ROOT / "library-panels" / "*.py"))
}

COUNTRIES = {"US", "EA", "GB", "JP", "CN"}


def _targets(obj):
    """Recursively yield every core-datasource query target."""
    if isinstance(obj, dict):
        if obj.get("datasource", {}).get("type") == "core-datasource":
            for t in obj.get("targets", []):
                yield t
        for v in obj.values():
            yield from _targets(v)
    elif isinstance(obj, list):
        for v in obj:
            yield from _targets(v)


def _all_jsons():
    files = glob.glob(str(ROOT / "dashboards" / "*.json")) + glob.glob(
        str(ROOT / "library-panels" / "*.json")
    )
    assert files, "No dashboard or library-panel JSON files found"
    return files


def test_every_target_has_a_valid_ref_or_inline_source():
    for f in _all_jsons():
        with open(f) as fh:
            d = json.load(fh)
        for t in _targets(d):
            ref = (t.get("ref") or "").strip()
            src = (t.get("source") or "").strip()
            if ref:
                plugin, _, metric = ref.partition("/")
                assert plugin == "basic-data-app", f"{f}: wrong plugin prefix in ref '{ref}'"
                assert metric in METRICS, f"{f}: ref '{ref}' has no library-panels/{metric}.py"
            else:
                assert src and "@metric" in src, (
                    f"{f}: target has neither a valid ref nor an inline @metric source"
                )


def test_macro_dashboards_have_per_country_panels():
    for name in ("world-macro.json", "macro-compare.json"):
        path = ROOT / "dashboards" / name
        assert path.exists(), f"dashboards/{name} not found"
        with open(path) as fh:
            d = json.load(fh)
        countries = {
            p["title"].rsplit("—", 1)[-1].strip()
            for p in d.get("panels", [])
            if p.get("type") != "row" and "—" in p.get("title", "")
        }
        # Each metric is rendered once per country -> per-country panels exist.
        assert {"US", "EA", "GB", "JP"} <= countries, (
            f"{name}: expected per-country panels (US/EA/GB/JP), got {countries}"
        )
        assert countries <= COUNTRIES, f"{name}: unexpected country in titles: {countries}"
