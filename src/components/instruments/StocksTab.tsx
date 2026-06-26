import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { css } from '@emotion/css';

import { AppEvents, type GrafanaTheme2 } from '@grafana/data';
import { Badge, Icon, Select, Stack, useStyles2 } from '@grafana/ui';

import { appEvents } from '../../lib/toast';
import {
  changeYahooSymbol,
  listInstrumentsUsed,
  type BackfillState,
  type InstrumentUsedRow,
} from '../../api/yfinance';
import { lookupSymbol, type LookupCandidate } from '../../api/reference';
import {
  CANONICAL_SECTORS,
  CANONICAL_INDUSTRIES,
  mergeClassificationOptions,
  setClassification,
} from '../../api/classification';

const LIVE_TICK_MAX_AGE_SEC = 60;
const STALE_THRESHOLD_HOURS = 36;
const LOOKUP_CONCURRENCY = 4;

const isActive = (r: InstrumentUsedRow): boolean =>
  r.backfill_state === 'pending' ||
  r.backfill_state === 'running' ||
  r.latest_run_status === 'running';

type CandidatesState = {
  loading: boolean;
  candidates: LookupCandidate[] | null;
  error: string | null;
};

/** Stable key for a (instrument, portfolio) pair. */
function pairKey(row: InstrumentUsedRow): string {
  return `${row.instrument_id}|${row.portfolio_id}`;
}

/**
 * Per-instrument-portfolio operator console.
 *
 * One table row per (instrument, portfolio) pair. Each row exposes the
 * derived Yahoo ticker as a dropdown; changing the dropdown upserts the
 * row in `instrument_ticker_mapping` AND triggers the in-process
 * discovery loop to backfill the new symbol. The Status column compounds
 * backfill state + coverage + live-tick recency so the operator can see
 * at a glance whether a pair is healthy, stale, or never mapped.
 *
 * Auto-lookup on first render: for every equity pair without a
 * mapping row, calls `lookupSymbol(instrument_id)` in a small concurrent
 * pool (4 in-flight). If the top match symbol-matches the instrument_id
 * case-insensitively (or is the only result), auto-set it. Operators can
 * always override via the dropdown.
 */
export function StocksTab() {
  const styles = useStyles2(getStyles);
  const [rows, setRows] = useState<InstrumentUsedRow[] | null>(null);
  const [candidatesByPair, setCandidatesByPair] = useState<Record<string, CandidatesState>>({});
  const [pendingPatches, setPendingPatches] = useState<Record<string, boolean>>({});
  const [portfolioFilter, setPortfolioFilter] = useState<string | null>(null);
  // Per-pair "we've already tried" set, not a global once-per-mount latch.
  const attemptedAutoLookup = useRef<Set<string>>(new Set());

  const refresh = useCallback(async (): Promise<InstrumentUsedRow[]> => {
    try {
      const data = await listInstrumentsUsed();
      setRows(data);
      return data;
    } catch (err) {
      appEvents.emit(AppEvents.alertError, [
        err instanceof Error ? err.message : 'Failed to load instruments',
      ]);
      setRows([]);
      return [];
    }
  }, []);

  useEffect(() => {
    let stopped = false;
    let handle = 0;
    const tick = async () => {
      const data = await refresh();
      if (stopped) { return; }
      const active = data.some(isActive);
      handle = window.setTimeout(tick, active ? 2_500 : 8_000);
    };
    void tick();
    return () => {
      stopped = true;
      window.clearTimeout(handle);
    };
  }, [refresh]);

  // --- candidate fetching (per-pair, lazy + auto-on-mount) -----------------

  const fetchCandidates = useCallback(
    async (row: InstrumentUsedRow): Promise<LookupCandidate[] | null> => {
      const key = pairKey(row);
      setCandidatesByPair((cur) => ({
        ...cur,
        [key]: {
          loading: true,
          candidates: cur[key]?.candidates ?? null,
          error: null,
        },
      }));
      try {
        const results = await lookupSymbol(row.instrument_id, 10);
        setCandidatesByPair((cur) => ({
          ...cur,
          [key]: { loading: false, candidates: results, error: null },
        }));
        return results;
      } catch (err) {
        const message = err instanceof Error ? err.message : 'Lookup failed';
        setCandidatesByPair((cur) => ({
          ...cur,
          [key]: { loading: false, candidates: null, error: message },
        }));
        return null;
      }
    },
    [],
  );

  const applySymbol = useCallback(
    async (row: InstrumentUsedRow, symbol: string, opts: { silent?: boolean } = {}) => {
      if (!symbol || symbol === row.yahoo_symbol) {
        return;
      }
      const key = pairKey(row);
      setPendingPatches((cur) => ({ ...cur, [key]: true }));
      try {
        await changeYahooSymbol(row.instrument_id, row.portfolio_id, symbol);
        if (!opts.silent) {
          const pname = row.portfolio_name || row.portfolio_id;
          appEvents.emit(AppEvents.alertSuccess, [
            `Mapped ${row.instrument_id} (${pname}) → ${symbol}. Stale prices purged; backfill enqueued.`,
          ]);
        }
        await refresh();
        window.setTimeout(() => void refresh(), 1_500);
        window.setTimeout(() => void refresh(), 4_000);
      } catch (err) {
        appEvents.emit(AppEvents.alertError, [
          err instanceof Error ? err.message : 'Symbol change failed',
        ]);
      } finally {
        setPendingPatches((cur) => {
          const next = { ...cur };
          delete next[key];
          return next;
        });
      }
    },
    [refresh],
  );

  const applyClassification = useCallback(
    async (row: InstrumentUsedRow, patch: { sector?: string | null; industry?: string | null }) => {
      const key = pairKey(row);
      setPendingPatches((cur) => ({ ...cur, [key]: true }));
      try {
        await setClassification(row.instrument_id, row.portfolio_id, patch);
        await refresh();
      } catch (err) {
        appEvents.emit(AppEvents.alertError, [
          err instanceof Error ? err.message : 'Classification change failed',
        ]);
      } finally {
        setPendingPatches((cur) => {
          const next = { ...cur };
          delete next[key];
          return next;
        });
      }
    },
    [refresh],
  );

  // Auto-lookup pass. Runs every time `rows` changes, picking up any
  // newly-discovered unmapped equity pair. Per-pair `attemptedAutoLookup` set
  // prevents re-firing for a pair already tried. FX pairs are
  // skipped — their Yahoo symbol is derived deterministically from the
  // instrument_id pattern (FX:GBPUSD → GBPUSD=X).
  useEffect(() => {
    if (!rows) {
      return;
    }
    const targets = rows.filter(
      (r) =>
        !r.yahoo_symbol &&
        r.kind === 'equity' &&
        !attemptedAutoLookup.current.has(pairKey(r)),
    );
    if (targets.length === 0) {
      return;
    }
    for (const t of targets) {
      attemptedAutoLookup.current.add(pairKey(t));
    }

    const queue = [...targets];
    const workers = Array.from({ length: LOOKUP_CONCURRENCY }, async () => {
      while (queue.length > 0) {
        const row = queue.shift();
        if (!row) { return; }
        const candidates = await fetchCandidates(row);
        if (!candidates || candidates.length === 0) {
          continue;
        }
        const top = candidates[0];
        await applySymbol(row, top.symbol, { silent: true });
      }
    });
    void Promise.all(workers).then(() => void refresh());
  }, [rows, fetchCandidates, applySymbol, refresh]);

  // portfolio_id → display name (falls back to the id when unknown).
  const portfolioNameById = useMemo(() => {
    const m = new Map<string, string>();
    for (const r of rows ?? []) {
      m.set(r.portfolio_id, r.portfolio_name || r.portfolio_id);
    }
    return m;
  }, [rows]);

  // Filter options: value = id (stable), label = name. Sorted by name so the
  // dropdown reads naturally.
  const portfolioOptions = useMemo(
    () =>
      Array.from(portfolioNameById.entries())
        .map(([id, name]) => ({ label: name, value: id }))
        .sort((a, b) => a.label.localeCompare(b.label)),
    [portfolioNameById],
  );

  const didAutoSelectPortfolio = useRef(false);
  useEffect(() => {
    if (!didAutoSelectPortfolio.current && portfolioFilter === null && portfolioOptions.length > 0) {
      didAutoSelectPortfolio.current = true;
      setPortfolioFilter(portfolioOptions[0].value);
    }
  }, [portfolioOptions, portfolioFilter]);
  // A portfolio must be selected before instruments are shown — the table is
  // per-portfolio, and showing every portfolio's instruments at once is
  // misleading. No selection => empty (the prompt below renders instead).
  // Deterministic order by instrument_id. The backend list query has no
  // ORDER BY, so each poll/refresh can return pairs in a different order;
  // without this the rows visibly reshuffle on every refresh (and on the
  // refresh storm a ticker change triggers). Sorting on a stable key keeps
  // each row in place.
  const visibleRows = useMemo(
    () =>
      portfolioFilter
        ? (rows ?? [])
            .filter((r) => r.portfolio_id === portfolioFilter)
            .slice()
            .sort((a, b) => a.instrument_id.localeCompare(b.instrument_id))
        : [],
    [rows, portfolioFilter],
  );

  const totalRows = visibleRows.length;
  const mappedCount = visibleRows.filter((r) => !!r.yahoo_symbol).length;

  const storedSectors = useMemo(
    () => Array.from(new Set((rows ?? []).map((r) => r.sector).filter(Boolean) as string[])),
    [rows],
  );
  const storedIndustries = useMemo(
    () => Array.from(new Set((rows ?? []).map((r) => r.industry).filter(Boolean) as string[])),
    [rows],
  );

  return (
    <>
      <div className={styles.header}>
        <div>
          <h2 className={styles.h2}>Yahoo mappings</h2>
          <p className={styles.sub}>
            Each (instrument, portfolio) pair observed in your portfolios appears here. Pick a
            Yahoo ticker from the dropdown — backfill kicks in automatically.
          </p>
        </div>
        <div className={styles.counters}>
          <div style={{ minWidth: 240 }}>
            <Select
              isClearable
              placeholder="All portfolios"
              value={portfolioFilter}
              options={portfolioOptions}
              onChange={(v) => setPortfolioFilter(v?.value ?? null)}
            />
          </div>
          <Counter label="Mapped" value={mappedCount} accent="primary" />
          <Counter label="Total" value={totalRows} />
        </div>
      </div>

      <div className={styles.tableWrapper}>
        {rows === null && <div className={styles.muted}>Loading…</div>}
        {rows !== null && rows.length === 0 && (
          <div className={styles.empty}>
            <Icon name="info-circle" /> No instruments observed yet. Create a portfolio and
            import trades from the Portfolio Admin plugin.
          </div>
        )}
        {rows !== null && rows.length > 0 && !portfolioFilter && (
          <div className={styles.empty}>
            <Icon name="info-circle" /> Select a portfolio from the dropdown above to view its
            instruments.
          </div>
        )}
        {rows !== null && rows.length > 0 && portfolioFilter && (
          <table className={styles.table}>
            <thead>
              <tr>
                <th>Instrument</th>
                <th>Kind</th>
                <th>CCY</th>
                <th>Yahoo ticker</th>
                <th>Sector</th>
                <th>Industry</th>
                <th>Status</th>
                <th>Coverage</th>
                <th>Last data</th>
              </tr>
            </thead>
            <tbody>
              {visibleRows.map((row) => {
                const key = pairKey(row);
                return (
                  <RowItem
                    key={key}
                    row={row}
                    candidates={candidatesByPair[key]}
                    pending={!!pendingPatches[key]}
                    onOpenDropdown={() => {
                      if (!candidatesByPair[key]?.candidates) {
                        void fetchCandidates(row);
                      }
                    }}
                    onPickSymbol={(symbol) => void applySymbol(row, symbol)}
                    storedSectors={storedSectors}
                    storedIndustries={storedIndustries}
                    onSetClassification={(patch) => void applyClassification(row, patch)}
                  />
                );
              })}
            </tbody>
          </table>
        )}
      </div>
    </>
  );
}

type RowProps = {
  row: InstrumentUsedRow;
  candidates: CandidatesState | undefined;
  pending: boolean;
  onOpenDropdown: () => void;
  onPickSymbol: (symbol: string) => void;
  storedSectors: string[];
  storedIndustries: string[];
  onSetClassification: (patch: { sector?: string | null; industry?: string | null }) => void;
};

function RowItem({
  row,
  candidates,
  pending,
  onOpenDropdown,
  onPickSymbol,
  storedSectors,
  storedIndustries,
  onSetClassification,
}: RowProps) {
  const styles = useStyles2(getStyles);

  const options = useMemo(() => {
    const base: Array<{ label: string; value: string; description?: string }> = [];
    if (candidates?.candidates) {
      for (const c of candidates.candidates) {
        const description = [c.short_name, c.exchange].filter(Boolean).join(' · ') || undefined;
        base.push({ label: c.symbol, value: c.symbol, description });
      }
    }
    if (row.yahoo_symbol && !base.some((o) => o.value === row.yahoo_symbol)) {
      base.unshift({ label: row.yahoo_symbol, value: row.yahoo_symbol, description: 'current' });
    }
    return base;
  }, [candidates, row.yahoo_symbol]);

  const sectorOptions = useMemo(
    () => mergeClassificationOptions(CANONICAL_SECTORS, storedSectors, row.sector),
    [storedSectors, row.sector],
  );
  const industryOptions = useMemo(
    () => mergeClassificationOptions(CANONICAL_INDUSTRIES, storedIndustries, row.industry),
    [storedIndustries, row.industry],
  );

  return (
    <tr>
      <td>
        <strong>{row.instrument_id}</strong>
      </td>
      <td>
        <KindBadge kind={row.kind} baseCcy={row.base_currency} quoteCcy={row.currency} />
      </td>
      <td>{row.currency ?? '—'}</td>
      <td className={styles.pickerCell}>
        <Select
          width={30}
          isLoading={candidates?.loading || pending}
          options={options}
          value={row.yahoo_symbol ?? undefined}
          onOpenMenu={onOpenDropdown}
          onChange={(opt) => {
            if (opt?.value) {
              onPickSymbol(opt.value);
            }
          }}
          placeholder={row.kind === 'fx_pair' ? '(derived)' : 'Pick a Yahoo ticker'}
          allowCustomValue
          onCreateOption={(value) => onPickSymbol(value.trim())}
          noOptionsMessage="No matches"
        />
      </td>
      <td className={styles.pickerCell}>
        <Select
          width={28}
          isLoading={pending}
          options={sectorOptions}
          value={row.sector ?? undefined}
          onChange={(opt) => {
            if (opt?.value) {
              onSetClassification({ sector: opt.value });
            }
          }}
          placeholder="Sector"
          allowCustomValue
          isClearable={false}
          onCreateOption={(value) => onSetClassification({ sector: value.trim() })}
          noOptionsMessage="No sectors"
        />
      </td>
      <td className={styles.pickerCell}>
        <Select
          width={28}
          isLoading={pending}
          options={industryOptions}
          value={row.industry ?? undefined}
          onChange={(opt) => {
            if (opt?.value) {
              onSetClassification({ industry: opt.value });
            }
          }}
          placeholder="Industry"
          allowCustomValue
          isClearable={false}
          onCreateOption={(value) => onSetClassification({ industry: value.trim() })}
          noOptionsMessage="No industries"
        />
      </td>
      <td>
        <StatusCell row={row} />
      </td>
      <td>
        <Coverage row={row} />
      </td>
      <td>
        <LiveTick row={row} />
      </td>
    </tr>
  );
}

function KindBadge({
  kind,
  baseCcy,
  quoteCcy,
}: {
  kind: string | null;
  baseCcy: string | null;
  quoteCcy: string | null;
}) {
  if (kind === 'fx_pair') {
    return <Badge color="purple" text={`fx · ${baseCcy ?? '—'} → ${quoteCcy ?? '—'}`} />;
  }
  return <Badge color="blue" text={kind ?? 'equity'} />;
}

function StatusCell({ row }: { row: InstrumentUsedRow }) {
  const state = deriveStatus(row);
  return (
    <Stack direction="row" gap={0.5} wrap>
      <Badge color={state.color} text={state.label} icon={state.icon} />
      {row.last_tick_age_sec !== null && row.last_tick_age_sec <= LIVE_TICK_MAX_AGE_SEC && (
        <Badge color="green" text="live" icon="signal" />
      )}
    </Stack>
  );
}

type StatusPresentation = {
  label: string;
  color: 'green' | 'red' | 'blue' | 'orange' | 'darkgrey' | 'purple';
  icon?: 'check' | 'sync' | 'hourglass' | 'exclamation-triangle' | 'circle' | 'history';
};

function deriveStatus(row: InstrumentUsedRow): StatusPresentation {
  if (!row.yahoo_symbol) {
    return { label: 'unmapped', color: 'darkgrey', icon: 'circle' };
  }
  const bs: BackfillState = row.backfill_state;
  if (bs === 'pending') {
    return { label: 'pending', color: 'orange', icon: 'hourglass' };
  }
  if (bs === 'running') {
    return { label: 'backfilling', color: 'blue', icon: 'sync' };
  }
  if (bs === 'failed') {
    return { label: 'failed', color: 'red', icon: 'exclamation-triangle' };
  }
  if (bs === 'done') {
    const lastObs = row.last_observed_at;
    if (lastObs === null) {
      return { label: 'done · no data', color: 'darkgrey', icon: 'circle' };
    }
    const ageH = (Date.now() * 1000 - lastObs) / 3_600_000_000;
    if (ageH > STALE_THRESHOLD_HOURS) {
      return { label: 'stale', color: 'orange', icon: 'history' };
    }
    return { label: 'up-to-date', color: 'green', icon: 'check' };
  }
  return { label: 'idle', color: 'darkgrey', icon: 'circle' };
}

function Coverage({ row }: { row: InstrumentUsedRow }) {
  const lastStr = toShortDate(row.last_observed_at);
  const startStr = toShortDate(row.backfill_start_ts);
  if (!lastStr && !startStr) {
    return <span className={fmt.muted}>—</span>;
  }
  if (!lastStr) {
    return <span className={fmt.mono}>{startStr ?? '—'} → —</span>;
  }
  return (
    <span className={fmt.mono}>
      {startStr ?? '—'} → {lastStr}
    </span>
  );
}

function LiveTick({ row }: { row: InstrumentUsedRow }) {
  const us = row.last_data_at;
  if (typeof us !== 'number' || !Number.isFinite(us)) {
    return <span className={fmt.muted}>—</span>;
  }
  // eslint-disable-next-line react-hooks/purity -- relative "age ago" label intentionally reads the wall clock
  const sec = (Date.now() * 1000 - us) / 1_000_000;
  return <span className={fmt.mono}>{ageHuman(sec)} ago</span>;
}

function ageHuman(sec: number): string {
  if (sec < 60) { return `${Math.round(sec)}s`; }
  if (sec < 3600) { return `${Math.round(sec / 60)}m`; }
  if (sec < 86400) { return `${Math.round(sec / 3600)}h`; }
  return `${Math.round(sec / 86400)}d`;
}

/** Format a microsecond timestamp as `YYYY-MM-DD`. Returns `null` when the
 * value is missing/invalid so callers can render their own placeholder
 * without risking a `RangeError` from the `Date` constructor. */
function toShortDate(micros: number | null | undefined): string | null {
  if (typeof micros !== 'number' || !Number.isFinite(micros) || micros <= 0) {
    return null;
  }
  const d = new Date(micros / 1000);
  if (Number.isNaN(d.getTime())) {
    return null;
  }
  return d.toISOString().slice(0, 10);
}

function Counter({
  label,
  value,
  accent,
}: {
  label: string;
  value: number;
  accent?: 'primary';
}) {
  const styles = useStyles2(getStyles);
  return (
    <div className={accent === 'primary' ? styles.counterPrimary : styles.counter}>
      <div className={styles.counterValue}>{value}</div>
      <div className={styles.counterLabel}>{label}</div>
    </div>
  );
}

const fmt = {
  muted: css({ color: 'gray' }),
  mono: css({ fontFamily: 'ui-monospace, monospace', fontSize: 12 }),
};

const getStyles = (theme: GrafanaTheme2) => ({
  header: css({
    display: 'flex',
    justifyContent: 'space-between',
    alignItems: 'flex-end',
    marginBottom: theme.spacing(2),
    gap: theme.spacing(2),
    flexWrap: 'wrap',
  }),
  h2: css({
    margin: 0,
    fontSize: theme.typography.h3.fontSize,
  }),
  sub: css({
    color: theme.colors.text.secondary,
    marginTop: theme.spacing(0.5),
    marginBottom: 0,
    maxWidth: 640,
  }),
  counters: css({
    display: 'flex',
    gap: theme.spacing(1.5),
  }),
  counter: css({
    padding: theme.spacing(1, 2),
    backgroundColor: theme.colors.background.secondary,
    border: `1px solid ${theme.colors.border.weak}`,
    borderRadius: theme.shape.radius.default,
    minWidth: 80,
    textAlign: 'center',
  }),
  counterPrimary: css({
    padding: theme.spacing(1, 2),
    backgroundColor: theme.colors.primary.transparent,
    border: `1px solid ${theme.colors.primary.border}`,
    borderRadius: theme.shape.radius.default,
    minWidth: 80,
    textAlign: 'center',
  }),
  counterValue: css({
    fontSize: theme.typography.h3.fontSize,
    fontWeight: theme.typography.fontWeightMedium,
  }),
  counterLabel: css({
    fontSize: theme.typography.bodySmall.fontSize,
    color: theme.colors.text.secondary,
    textTransform: 'uppercase',
    letterSpacing: '0.5px',
  }),
  tableWrapper: css({
    backgroundColor: theme.colors.background.primary,
    border: `1px solid ${theme.colors.border.weak}`,
    borderRadius: theme.shape.radius.default,
    overflowX: 'auto',
  }),
  table: css({
    width: '100%',
    borderCollapse: 'collapse',
    fontSize: theme.typography.body.fontSize,
    'th, td': {
      padding: theme.spacing(1, 1.5),
      borderBottom: `1px solid ${theme.colors.border.weak}`,
      verticalAlign: 'middle',
      textAlign: 'left',
    },
    th: {
      backgroundColor: theme.colors.background.secondary,
      color: theme.colors.text.secondary,
      fontWeight: theme.typography.fontWeightMedium,
      fontSize: theme.typography.bodySmall.fontSize,
      textTransform: 'uppercase',
      letterSpacing: '0.5px',
    },
    'tbody tr:hover': {
      backgroundColor: theme.colors.background.secondary,
    },
  }),
  pickerCell: css({
    minWidth: 240,
  }),
  empty: css({
    padding: theme.spacing(4),
    textAlign: 'center',
    color: theme.colors.text.secondary,
  }),
  muted: css({
    padding: theme.spacing(2),
    color: theme.colors.text.secondary,
  }),
});
