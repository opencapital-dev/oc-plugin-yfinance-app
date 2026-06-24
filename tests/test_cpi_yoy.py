from pathlib import Path

import polars as pl

LIBRARY_PANELS = Path(__file__).resolve().parent.parent / "library-panels"


def _run(source: str, country: str, fake_fetch, fake_pg):
    src = source.replace("$country", country)
    captured = {}
    def metric(*, output):
        def deco(fn): captured["fn"] = fn; captured["output"] = output; return fn
        return deco
    ns = {"metric": metric, "pl": pl, "fetch_json": fake_fetch, "pg": fake_pg, "window": (0, 1)}
    exec(src, ns)
    return captured["fn"]()


def test_cpi_yoy_us_uses_fred_and_computes_yoy():
    src = (LIBRARY_PANELS / "cpi_yoy.py").read_text()
    def fake_pg(q, *a): return pl.DataFrame({"value": ["KEY"]})
    def fake_fetch(url, **kw):
        # 13 monthly index points 100..112 → last YoY = 12%
        return {"observations": [{"date": f"2023-{m:02d}-01", "value": str(100 + i)}
                                 for i, m in enumerate(range(1, 13))]
                + [{"date": "2024-01-01", "value": "112"}]}
    df = _run(src, "US", fake_fetch, fake_pg)
    assert abs(df.sort("ts")["value"][-1] - 12.0) < 1e-6
