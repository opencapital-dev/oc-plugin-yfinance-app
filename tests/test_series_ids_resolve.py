"""
D3: Live verification gate for macro metric series ids.

Verifies every DBnomics path and FRED id used by the 7 macro metric panels
resolves against the real APIs and returns observations.

Run with:
    pytest tests/test_series_ids_resolve.py -v -m integration -k dbnomics
    FRED_API_KEY=<key> pytest tests/test_series_ids_resolve.py -v -m integration -k fred
"""
import os
import json
import urllib.request
import urllib.error

import pytest

pytestmark = pytest.mark.integration

FRED_KEY = os.environ.get("FRED_API_KEY")

# ---------------------------------------------------------------------------
# Complete inventory of every DBnomics path used in library-panels/*.py
# Sources: policy_rate.py, curve_slope.py, gdp_yoy.py, cpi_yoy.py,
#          yield_10y.py, unemployment.py, real_rate.py
# ---------------------------------------------------------------------------
DBN = [
    # --- policy_rate.py + real_rate.py POLICY ---
    "ECB/FM/D.U2.EUR.4F.KR.MRR_FR.LEV",                          # EA policy rate
    "BIS/WS_CBPOL/D.GB",                                          # GB policy rate (was BOE/IUDBEDR/IUDBEDR)
    "BIS/WS_CBPOL/D.JP",                                          # JP policy rate (was BIS/cbpol/D.JP)
    "BIS/WS_CBPOL/D.CN",                                          # CN policy rate (was BIS/cbpol/D.CN)

    # --- yield_10y.py + curve_slope.py TEN ---
    "Eurostat/irt_lt_mcby_d/D.MCBY.EA",                           # EA 10y yield (was D.EA)
    "OECD/DSD_STES@DF_FINMARK/GBR.M.IRLT.PA._Z._Z._Z._Z.N",     # GB 10y yield (was MEI_FIN/IRLT.GBR.M)
    "OECD/DSD_STES@DF_FINMARK/JPN.M.IRLT.PA._Z._Z._Z._Z.N",     # JP 10y yield
    "OECD/DSD_STES@DF_FINMARK/CHN.M.IRLT.PA._Z._Z._Z._Z.N",     # CN 10y yield

    # --- curve_slope.py TWO ---
    "Eurostat/irt_st_m/M.IRT_M3.EA",                              # EA 3m rate
    "OECD/DSD_STES@DF_FINMARK/GBR.M.IR3TIB.PA._Z._Z._Z._Z.N",   # GB 3m rate (was MEI_FIN/IR3TIB.GBR.M)
    "OECD/DSD_STES@DF_FINMARK/JPN.M.IR3TIB.PA._Z._Z._Z._Z.N",   # JP 3m rate
    "OECD/DSD_STES@DF_FINMARK/CHN.M.IR3TIB.PA._Z._Z._Z._Z.N",   # CN 3m rate

    # --- gdp_yoy.py ---
    "Eurostat/namq_10_gdp/Q.CLV10_MEUR.SCA.B1GQ.EA19",           # EA GDP quarterly
    "OECD/QNA/GBR.B1_GE.LNBQRSA.Q",                              # GB GDP quarterly
    "OECD/QNA/JPN.B1_GE.LNBQRSA.Q",                              # JP GDP quarterly
    # UNRESOLVED: CN GDP quarterly — China not in OECD/QNA. Best alternative is
    # NBS/Q_A0102/A010201 (constant-price levels, 2024+ only) or NBS/Q_A0103/A010301
    # (YoY index, preceding year=100). Neither fits _yoy() without code changes.

    # --- cpi_yoy.py + real_rate.py CPI ---
    "Eurostat/prc_hicp_midx/M.I15.CP00.EA",                       # EA CPI
    "OECD/DSD_G20_PRICES@DF_G20_PRICES/GBR.M.HICP.CPI.IX._T.N._Z",  # GB CPI (was PRICES_CPI/GBR.CPALTT01.IXOB.M)
    "OECD/DSD_G20_PRICES@DF_G20_PRICES/JPN.M.N.CPI.IX._T.N._Z",     # JP CPI
    "OECD/DSD_G20_PRICES@DF_G20_PRICES/CHN.M.N.CPI.IX._T.N._Z",     # CN CPI

    # --- unemployment.py ---
    "Eurostat/une_rt_m/M.SA.TOTAL.PC_ACT.T.EA20",                  # EA unemployment (was EA19)
    "OECD/DSD_LFS@DF_IALFS_UNE_M/GBR.UNE_LF_M.PT_LF_SUB._Z.Y._T.Y_GE15._Z.M",  # GB unemployment SA
    "OECD/DSD_LFS@DF_IALFS_UNE_M/JPN.UNE_LF_M.PT_LF_SUB._Z.Y._T.Y_GE15._Z.M",  # JP unemployment SA
    # UNRESOLVED: CN unemployment monthly — China not in OECD DSD_LFS; no accessible
    # ILO monthly series confirmed. NBS A_A040N is registered unemployment (quarterly,
    # not ILO-methodology). Operator must source or accept panel error for CN.
]

# FRED ids — well-known-correct, skipped without FRED_API_KEY
FRED = ["CPIAUCSL", "GDPC1", "UNRATE", "DFF", "DGS10", "DGS2"]


@pytest.mark.parametrize("path", DBN, ids=DBN)
def test_dbnomics_resolves(path):
    """Each DBnomics path must return at least one series with observations."""
    url = f"https://api.db.nomics.world/v22/series/{path}?observations=1"
    try:
        resp = urllib.request.urlopen(url, timeout=20)
    except urllib.error.HTTPError as e:
        pytest.fail(f"HTTP {e.code} for {path}")
    except urllib.error.URLError as e:
        pytest.skip(f"Network unavailable: {e.reason}")

    js = json.load(resp)
    docs = js.get("series", {}).get("docs", [])
    assert docs, f"no series docs returned for {path}"
    assert docs[0].get("value"), f"no observations in series for {path}"


@pytest.mark.skipif(not FRED_KEY, reason="FRED_API_KEY not set")
@pytest.mark.parametrize("sid", FRED, ids=FRED)
def test_fred_resolves(sid):
    """Each FRED series id must return observations when FRED_API_KEY is set."""
    url = (
        f"https://api.stlouisfed.org/fred/series/observations"
        f"?series_id={sid}&api_key={FRED_KEY}&file_type=json"
    )
    try:
        resp = urllib.request.urlopen(url, timeout=20)
    except urllib.error.HTTPError as e:
        pytest.fail(f"HTTP {e.code} for FRED:{sid}")
    except urllib.error.URLError as e:
        pytest.skip(f"Network unavailable: {e.reason}")

    js = json.load(resp)
    assert js.get("observations"), f"no observations for FRED:{sid}"
