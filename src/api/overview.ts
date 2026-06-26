import { yfRequest } from './client';

export type Overview = {
  held_equities: number;
  held_options: number;
  option_underlyings: number;
  last_option_mark_us: number;
};

export const getOverview = () => yfRequest<Overview>('/overview');
