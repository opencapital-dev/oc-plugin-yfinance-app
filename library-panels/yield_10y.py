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
    "US": ("fred", "DGS10"),
    "EA": ("dbnomics", "Eurostat/irt_lt_mcby_d/D.MCBY.EA"),                       # was D.EA (404; series code is D.MCBY.EA)
    "GB": ("dbnomics", "OECD/DSD_STES@DF_FINMARK/GBR.M.IRLT.PA._Z._Z._Z._Z.N"), # was OECD/MEI_FIN/IRLT.GBR.M (404; MEI_FIN → DSD_STES@DF_FINMARK)
    "JP": ("dbnomics", "OECD/DSD_STES@DF_FINMARK/JPN.M.IRLT.PA._Z._Z._Z._Z.N"), # was OECD/MEI_FIN/IRLT.JPN.M
    "CN": ("dbnomics", "OECD/DSD_STES@DF_FINMARK/CHN.M.IRLT.PA._Z._Z._Z._Z.N"), # was OECD/MEI_FIN/IRLT.CHN.M
}

@metric(output="series")
def yield_10y():
    p, c = SERIES["$country"]; return _series(p, c).select("ts", "value")
