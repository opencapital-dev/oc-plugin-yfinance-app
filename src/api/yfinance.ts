import { yfRequest } from './client';

export type BarSize = '1m' | '1h' | '1d';

export type JobStatus = 'pending' | 'running' | 'done' | 'failed';

export type BackfillState =
  | 'pending'
  | 'running'
  | 'done'
  | 'failed'
  | 'none';

export type InstrumentUsedRow = {
  instrument_id: string;
  portfolio_id: string;
  /** Display name from the portfolios table; null when unknown (RW down). */
  portfolio_name: string | null;
  first_seen_ts: number;
  last_seen_ts: number;
  event_count: number;
  yahoo_symbol: string | null;
  currency: string | null;
  kind: string | null;
  base_currency: string | null;
  sector: string | null;
  industry: string | null;
  subscribed: boolean;
  backfill_start_ts: number | null;
  backfill_state: BackfillState;
  latest_run_status: JobStatus | null;
  latest_run_rows_published: number | null;
  latest_run_error: string | null;
  latest_run_finished_at: number | null;
  latest_run_bar_size: BarSize | null;
  last_observed_at: number | null;
  last_tick_age_sec: number | null;
  /** Newest data point across all namespaces (live + backfill), epoch micros. */
  last_data_at: number | null;
};

export type FxPairUsedRow = {
  base_ccy: string;
  quote_ccy: string;
  first_seen_ts: number;
  last_seen_ts: number;
  event_count: number;
  instrument_id: string | null;
  yahoo_symbol: string | null;
  latest_job_id: string | null;
  latest_job_status: JobStatus | null;
  latest_job_bar_size: BarSize | null;
  latest_job_start_ts: number | null;
  latest_job_end_ts: number | null;
  latest_job_rows_published: number | null;
  latest_job_error: string | null;
  latest_job_created_at: number | null;
  latest_job_finished_at: number | null;
};

export type JobRow = {
  job_id: string;
  instrument_id: string;
  portfolio_id: string;
  bar_size: BarSize;
  start_ts: number;
  end_ts: number;
  status: JobStatus;
  error: string | null;
  rows_published: number | null;
  created_at: number;
  started_at: number | null;
  finished_at: number | null;
};

export type EnqueueBody = {
  instrument_id: string;
  portfolio_id: string;
  bar_size: BarSize;
  start: number;
  end: number;
};

export function listInstrumentsUsed() {
  return yfRequest<InstrumentUsedRow[]>('/instruments');
}

export function enqueueJob(body: EnqueueBody) {
  return yfRequest<JobRow>('/jobs/enqueue', { method: 'POST', body });
}

export type SymbolChangeResponse = {
  instrument_id: string;
  portfolio_id: string;
  symbol: string;
  sector: string | null;
  industry: string | null;
};

/**
 * Change a (instrument, portfolio) pair's Yahoo symbol. The Go plugin upserts
 * the row in `instrument_ticker_mapping` and, when the symbol actually
 * changes, purges that instrument's stale prices (backfilled bars + live
 * quotes) in one scoped delete. The in-process discovery loop picks the
 * change up on its next tick (≈15s) and re-backfills under the new symbol.
 * The UI should refresh `/yf/instruments` after this returns.
 */
export function changeYahooSymbol(instrumentId: string, portfolioId: string, symbol: string) {
  return yfRequest<SymbolChangeResponse>(
    `/symbols/${encodeURIComponent(instrumentId)}`,
    {
      method: 'POST',
      body: { symbol, portfolio_id: portfolioId, updated_by: 'yfinance-plugin' },
    },
  );
}
