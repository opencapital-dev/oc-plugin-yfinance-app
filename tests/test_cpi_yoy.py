import json
from pathlib import Path

import polars as pl

LIBRARY_PANELS = Path(__file__).resolve().parent.parent / "library-panels"


def assert_ts_contract(df):
    """Series contract: `ts` must be Int64 epoch microseconds AND the frame must
    be JSON-serializable.

    computeframe.ToFrame frames a numeric `ts` column as time.UnixMicro, and the
    sidecar serializes rows with json.dumps in _send_json. A polars Datetime `ts`
    yields Python datetime objects → json.dumps raises TypeError OUTSIDE the
    request try/except → the handler thread dies → the caller gets EOF (empty
    reply), not an error. Assert both here to catch that regression.
    """
    assert df["ts"].dtype == pl.Int64, f"ts must be Int64 epoch-us, got {df['ts'].dtype}"
    json.dumps([list(r) for r in df.rows()])  # raises on datetime / non-JSON cells


def _run(source: str, country_csv: str, fake_fetch, fake_sql):
    # Grafana interpolates ${country:csv} to e.g. "US,EA"; the metric returns one
    # column per country.
    src = source.replace("${country:csv}", country_csv)
    captured = {}
    def metric(*, output):
        def deco(fn): captured["fn"] = fn; captured["output"] = output; return fn
        return deco
    ns = {
        "metric": metric,
        "pl": pl,
        "fetch_json": fake_fetch,
        "sql": fake_sql,
        # Regression guard: pg() returns a QuerySpec-like tuple WITHOUT .is_empty,
        # so any future misuse of pg() instead of sql() will raise AttributeError.
        "pg": lambda q, *a: ("pg-spec", q, a),
        "window": (0, 1),
    }
    exec(src, ns)
    return captured["fn"]()


def test_cpi_yoy_us_uses_fred_and_computes_yoy():
    src = (LIBRARY_PANELS / "cpi_yoy.py").read_text()
    def fake_sql(q, *a): return pl.DataFrame({"value": ["KEY"]})
    def fake_fetch(url, **kw):
        # 13 monthly index points 100..112 → last YoY = 12%
        return {"observations": [{"date": f"2023-{m:02d}-01", "value": str(100 + i)}
                                 for i, m in enumerate(range(1, 13))]
                + [{"date": "2024-01-01", "value": "112"}]}
    df = _run(src, "US", fake_fetch, fake_sql)
    # wide frame: one column per selected country
    assert df.columns == ["ts", "US"]
    assert abs(df.sort("ts")["US"][-1] - 12.0) < 1e-6
    assert_ts_contract(df)
