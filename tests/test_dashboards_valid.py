"""
E1: Validate that all Grafana dashboard and library-panel JSON files are
    internally consistent and reference only known metric Python files.
"""
import json
import glob
import os
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

METRICS = {
    os.path.splitext(os.path.basename(p))[0]
    for p in glob.glob(str(ROOT / "library-panels" / "*.py"))
}


def _refs(obj):
    """Recursively yield all 'ref' strings found inside core-datasource targets."""
    if isinstance(obj, dict):
        if obj.get("datasource", {}).get("type") == "core-datasource":
            for t in obj.get("targets", []):
                r = t.get("ref")
                if r:
                    yield r
        for v in obj.values():
            yield from _refs(v)
    elif isinstance(obj, list):
        for v in obj:
            yield from _refs(v)


def test_every_dashboard_ref_points_at_an_existing_metric():
    dashboard_jsons = glob.glob(str(ROOT / "dashboards" / "*.json"))
    library_jsons = glob.glob(str(ROOT / "library-panels" / "*.json"))
    all_files = dashboard_jsons + library_jsons
    assert all_files, "No dashboard or library-panel JSON files found"

    for f in all_files:
        with open(f) as fh:
            d = json.load(fh)
        for ref in _refs(d):
            plugin, _, metric = ref.partition("/")
            assert plugin == "basic-data-app", f"{f}: wrong plugin prefix in ref '{ref}'"
            assert metric in METRICS, (
                f"{f}: ref '{ref}' points at '{metric}' but no library-panels/{metric}.py exists"
            )


def test_dashboards_parse_and_have_country_variable():
    world_macro_path = ROOT / "dashboards" / "world-macro.json"
    assert world_macro_path.exists(), "dashboards/world-macro.json not found"
    with open(world_macro_path) as fh:
        d = json.load(fh)
    names = [v["name"] for v in d.get("templating", {}).get("list", [])]
    assert "country" in names, (
        f"world-macro.json has no 'country' template variable; found: {names}"
    )
