import { yfRequest } from './client';

export type OptionUnderlying = {
  root: string;
  portfolio_id: string;
  symbol: string;
  subscribed: boolean;
  held_contracts: number;
};

export const listOptionUnderlyings = () => yfRequest<OptionUnderlying[]>('/option-underlyings');

export const setOptionUnderlyingSymbol = (root: string, portfolio_id: string, symbol: string) =>
  yfRequest<{ ok: boolean }>('/option-underlyings', { method: 'POST', body: { root, portfolio_id, symbol } });

export const toggleOptionUnderlying = (root: string, portfolio_id: string, subscribed: boolean) =>
  yfRequest<{ ok: boolean }>('/option-underlyings', { method: 'POST', body: { root, portfolio_id, subscribed } });
