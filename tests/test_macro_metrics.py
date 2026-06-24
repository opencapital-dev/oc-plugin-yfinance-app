import polars as pl
from tests.test_cpi_yoy import _run, LIBRARY_PANELS


def _sql_key(q, *a):
    return pl.DataFrame({"value": ["KEY"]})


def test_unemployment_us_passthrough_level():
    src = (LIBRARY_PANELS / "unemployment.py").read_text()

    def fetch(url, **kw):
        return {"observations": [{"date": "2024-01-01", "value": "3.7"},
                                 {"date": "2024-02-01", "value": "3.9"}]}

    df = _run(src, "US", fetch, _sql_key)
    assert df.sort("ts")["value"][-1] == 3.9


def test_curve_slope_us_is_10y_minus_2y():
    src = (LIBRARY_PANELS / "curve_slope.py").read_text()
    calls = {"n": 0}

    def fetch(url, **kw):
        calls["n"] += 1
        v = "4.0" if "DGS10" in str(kw.get("params")) else "4.5"
        return {"observations": [{"date": "2024-01-01", "value": v}]}

    df = _run(src, "US", fetch, _sql_key)
    assert abs(df["value"][0] - (-0.5)) < 1e-9
