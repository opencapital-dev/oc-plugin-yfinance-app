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


def test_single_macro_dashboard_with_country_var_and_inline_sources():
    path = ROOT / "dashboards" / "macro.json"
    assert path.exists(), "dashboards/macro.json not found"
    with open(path) as fh:
        d = json.load(fh)

    var_names = {v["name"] for v in d.get("templating", {}).get("list", [])}
    assert "country" in var_names, f"macro.json missing 'country' variable; got {var_names}"

    panels = [p for p in d.get("panels", []) if p.get("type") != "row"]
    assert len(panels) >= 5, f"expected >=5 metric panels, got {len(panels)}"
    for p in panels:
        src = (p.get("targets", [{}])[0].get("source") or "")
        # each panel renders all selected countries from the dropdown via ${country:csv}
        assert "${country:csv}" in src and "@metric" in src, (
            f"panel '{p.get('title')}' is not a multi-country inline @metric source"
        )
