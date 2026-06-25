import polars as pl
from tests.test_cpi_yoy import _run, LIBRARY_PANELS, assert_ts_contract


def _sql_key(q, *a):
    return pl.DataFrame({"value": ["KEY"]})


def test_unemployment_us_passthrough_level():
    src = (LIBRARY_PANELS / "unemployment.py").read_text()

    def fetch(url, **kw):
        return {"observations": [{"date": "2024-01-01", "value": "3.7"},
                                 {"date": "2024-02-01", "value": "3.9"}]}

    df = _run(src, "US", fetch, _sql_key)
    assert df.sort("ts")["US"][-1] == 3.9
    assert_ts_contract(df)


def test_curve_slope_us_is_10y_minus_3m():
    # Now uniform OECD DBnomics: 10Y (IRLT) - 3M (IR3TIB).
    src = (LIBRARY_PANELS / "curve_slope.py").read_text()

    def fetch(url, **kw):
        val = 4.0 if "IRLT" in url else 4.5  # 10Y vs 3M
        return {"series": {"docs": [{"period_start_day": ["2024-01-01"], "value": [val]}]}}

    df = _run(src, "US", fetch, _sql_key)
    assert abs(df["US"][0] - (-0.5)) < 1e-9
    assert_ts_contract(df)
