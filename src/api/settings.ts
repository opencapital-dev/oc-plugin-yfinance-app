import { resRequest } from './client';

export type Settings = {
  fred_api_key_set: boolean;
  pollIntervalSec: number;
  qps: number;
  burst: number;
  liveEnable: boolean;
  backfillEnable: boolean;
  optionPollEnable: boolean;
  optionPollIntervalSec: number;
};

export const getSettings = () => resRequest<Settings>('/settings');

export const putSettings = (body: Partial<Settings> & { fred_api_key?: string }) =>
  resRequest<{ ok: boolean }>('/settings', { method: 'PUT', body });

export const testFred = () =>
  resRequest<{ ok: boolean; status?: number }>('/settings/test-fred', { method: 'POST' });
