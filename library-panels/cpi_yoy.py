def _fred_key():
    r = sql("SELECT value FROM basic_data.app_settings WHERE key = $1", "fred_api_key")
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
              .with_columns(pl.col("ts").str.to_datetime("%Y-%m-%d", strict=False).dt.epoch("us"))
              .drop_nulls())

def _dbnomics(path):  # path = "Provider/dataset/series"
    js = fetch_json(f"https://api.db.nomics.world/v22/series/{path}?observations=1")
    doc = js["series"]["docs"][0]
    ts = doc.get("period_start_day") or doc["period"]
    def _num(v):
        try:
            return float(v)
        except (TypeError, ValueError):
            return None  # DBnomics gaps come through as null or "NA"
    return (pl.DataFrame({"ts": ts, "value": [_num(v) for v in doc["value"]]})
              .with_columns(pl.col("ts").str.to_datetime("%Y-%m-%d", strict=False).dt.epoch("us"))
              .drop_nulls())

def _series(provider, code):
    return _fred(code) if provider == "fred" else _dbnomics(code)

def _yoy(df, periods):  # periods: 12 monthly, 4 quarterly
    return (df.sort("ts")
              .with_columns((pl.col("value") / pl.col("value").shift(periods) * 100 - 100).alias("value"))
              .drop_nulls())


def _selected(series_map):
    # ${country:csv} is interpolated by Grafana to e.g. "US,EA,GB". Keep known codes;
    # fall back to all if interpolation did not run.
    raw = "${country:csv}"
    sel = [c.strip() for c in raw.replace("{", "").replace("}", "").split(",")]
    sel = [c for c in sel if c in series_map]
    return sel or list(series_map.keys())


def _wide(series_map, build):
    # One value column per selected country (named by code); skip a failing country.
    out = None
    for c in _selected(series_map):
        try:
            df = build(c).select("ts", pl.col("value").alias(c))
        except Exception:
            continue
        out = df if out is None else out.join(df, on="ts", how="full", coalesce=True)
    return (out if out is not None else pl.DataFrame({"ts": []}, schema={"ts": pl.Int64})).sort("ts")

# country -> (provider, series), monthly CPI index (YoY computed below)
SERIES = {
    "US": ("fred", "CPIAUCSL"),
    "EA": ("dbnomics", "Eurostat/prc_hicp_midx/M.I15.CP00.EA"),
    "GB": ("dbnomics", "OECD/DSD_G20_PRICES@DF_G20_PRICES/GBR.M.HICP.CPI.IX._T.N._Z"),  # was OECD/PRICES_CPI/GBR.CPALTT01.IXOB.M (404; MEI_FIN replaced by DSD_G20_PRICES)
    "JP": ("dbnomics", "OECD/DSD_G20_PRICES@DF_G20_PRICES/JPN.M.N.CPI.IX._T.N._Z"),      # was OECD/PRICES_CPI/JPN.CPALTT01.IXOB.M
    "CN": ("dbnomics", "OECD/DSD_G20_PRICES@DF_G20_PRICES/CHN.M.N.CPI.IX._T.N._Z"),      # was OECD/PRICES_CPI/CHN.CPALTT01.IXOB.M
}

@metric(output="series")
def cpi_yoy():
    return _wide(SERIES, lambda c: _yoy(_series(*SERIES[c]), 12))
