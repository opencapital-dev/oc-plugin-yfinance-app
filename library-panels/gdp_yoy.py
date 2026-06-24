def _fred_key():
    r = pg("SELECT value FROM basic_data.app_settings WHERE key = $1", "fred_api_key")
    if r.is_empty() or r["value"][0] in (None, ""):
        raise ValueError("FRED API key not set — add it in Basic Data → Settings")
    return r["value"][0]

def _fred(series_id):
    js = fetch_json("https://api.stlouisfed.org/fred/series/observations",
                    params={"series_id": series_id, "api_key": _fred_key(), "file_type": "json"})
    obs = js["observations"]
    ts = [o["date"] for o in obs]
    val = [None if o["value"] in (".", "") else float(o["value"]) for o in obs]
    return (pl.DataFrame({"ts": ts, "value": val})
              .with_columns(pl.col("ts").str.to_datetime("%Y-%m-%d", strict=False))
              .drop_nulls())

def _dbnomics(path):  # path = "Provider/dataset/series"
    js = fetch_json(f"https://api.db.nomics.world/v22/series/{path}?observations=1")
    doc = js["series"]["docs"][0]
    ts = doc.get("period_start_day") or doc["period"]
    return (pl.DataFrame({"ts": ts, "value": doc["value"]})
              .with_columns(pl.col("ts").str.to_datetime("%Y-%m-%d", strict=False),
                            pl.col("value").cast(pl.Float64, strict=False))
              .drop_nulls())

def _series(provider, code):
    return _fred(code) if provider == "fred" else _dbnomics(code)

def _yoy(df, periods):  # periods: 12 monthly, 4 quarterly
    return (df.sort("ts")
              .with_columns((pl.col("value") / pl.col("value").shift(periods) * 100 - 100).alias("value"))
              .drop_nulls())

SERIES = {
    "US": ("fred", "GDPC1"),
    "EA": ("dbnomics", "Eurostat/namq_10_gdp/Q.CLV10_MEUR.SCA.B1GQ.EA19"),
    "GB": ("dbnomics", "OECD/QNA/GBR.B1_GE.LNBQRSA.Q"),
    "JP": ("dbnomics", "OECD/QNA/JPN.B1_GE.LNBQRSA.Q"),
    # UNRESOLVED: China is not in OECD/QNA. NBS/Q_A0102/A010201 provides constant-price quarterly
    # levels but with sparse history (2024+); NBS/Q_A0103/A010301 gives YoY index (preceding
    # year=100) which would need different handling than _yoy(). Operator must decide approach.
    "CN": ("dbnomics", "OECD/QNA/CHN.B1_GE.LNBQRSA.Q"),
}

@metric(output="series")
def gdp_yoy():
    p, c = SERIES["$country"]; return _yoy(_series(p, c), 4).select("ts", "value")
