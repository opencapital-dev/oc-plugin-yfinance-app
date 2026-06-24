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

POLICY = {"US": ("fred", "DFF"), "EA": ("dbnomics", "ECB/FM/D.U2.EUR.4F.KR.MRR_FR.LEV"),
          "GB": ("dbnomics", "BIS/WS_CBPOL/D.GB"),   # was BOE/IUDBEDR/IUDBEDR (404)
          "JP": ("dbnomics", "BIS/WS_CBPOL/D.JP"),   # was BIS/cbpol/D.JP (404; dataset renamed WS_CBPOL)
          "CN": ("dbnomics", "BIS/WS_CBPOL/D.CN")}   # was BIS/cbpol/D.CN
CPI = {"US": ("fred", "CPIAUCSL"), "EA": ("dbnomics", "Eurostat/prc_hicp_midx/M.I15.CP00.EA"),
       "GB": ("dbnomics", "OECD/DSD_G20_PRICES@DF_G20_PRICES/GBR.M.HICP.CPI.IX._T.N._Z"),  # was OECD/PRICES_CPI/GBR.CPALTT01.IXOB.M (404)
       "JP": ("dbnomics", "OECD/DSD_G20_PRICES@DF_G20_PRICES/JPN.M.N.CPI.IX._T.N._Z"),      # was OECD/PRICES_CPI/JPN.CPALTT01.IXOB.M
       "CN": ("dbnomics", "OECD/DSD_G20_PRICES@DF_G20_PRICES/CHN.M.N.CPI.IX._T.N._Z")}      # was OECD/PRICES_CPI/CHN.CPALTT01.IXOB.M

@metric(output="series")
def real_rate():
    pol = _series(*POLICY["$country"]).sort("ts").rename({"value": "pol"})
    infl = _yoy(_series(*CPI["$country"]), 12).sort("ts").rename({"value": "cpi"})
    j = pol.join_asof(infl, on="ts")  # nearest prior CPI for each rate point
    return j.with_columns((pl.col("pol") - pl.col("cpi")).alias("value")).select("ts", "value").drop_nulls()
