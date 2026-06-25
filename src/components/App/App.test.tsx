import React from 'react';
import { MemoryRouter } from 'react-router-dom';
import { AppRootProps, PluginType } from '@grafana/data';
import { render, screen, waitFor } from '@testing-library/react';
import App from './App';

// Mock @grafana/runtime so the pages' getBackendSrv() / getDataSourceSrv()
// don't reach into a non-existent Grafana stack during unit tests.
jest.mock('@grafana/runtime', () => ({
  PluginPage: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  // Per-URL mock: instruments endpoints return arrays, settings returns an object.
  getBackendSrv: () => ({
    get: jest.fn().mockImplementation((url: string) => {
      if (url.includes('/settings')) {
        return Promise.resolve({ fred_api_key_set: false, pollIntervalSec: 15, qps: 1, burst: 3, liveEnable: true, backfillEnable: true });
      }
      if (url.includes('/overview')) {
        return Promise.resolve({ held_equities: 0, held_options: 0, option_underlyings: 0, last_option_mark_us: 0 });
      }
      return Promise.resolve([]);
    }),
    post: jest.fn().mockResolvedValue({}),
    put: jest.fn().mockResolvedValue({ ok: true }),
  }),
  getAppEvents: () => ({ publish: jest.fn() }),
}));

describe('Components/App', () => {
  let props: AppRootProps;

  beforeEach(() => {
    jest.resetAllMocks();
    props = {
      basename: 'a/yfinance-ingestor',
      meta: {
        id: 'basic-data-app',
        name: 'yFinance Data',
        type: PluginType.app,
        enabled: true,
        jsonData: {},
      },
      query: {},
      path: '',
      onNavChanged: jest.fn(),
    } as unknown as AppRootProps;
  });

  test('renders the Instruments route by default', async () => {
    const { queryAllByText } = render(
      <MemoryRouter initialEntries={['/instruments']}>
        <App {...props} />
      </MemoryRouter>
    );
    // The Instruments page renders a heading or empty-state we can wait on.
    await waitFor(() => expect(queryAllByText(/instruments|tickers|Yahoo/i).length).toBeGreaterThan(0), {
      timeout: 2000,
    });
  });

  test('redirects unknown routes to Overview', async () => {
    render(
      <MemoryRouter initialEntries={['/unknown-route']}>
        <App {...props} />
      </MemoryRouter>
    );
    // Overview page renders an "Overview" heading
    await waitFor(() => expect(screen.queryAllByText(/overview|holdings|option marks/i).length).toBeGreaterThan(0), {
      timeout: 2000,
    });
  });

  test('renders the Settings page at the settings route', async () => {
    render(
      <MemoryRouter initialEntries={['/settings']}>
        <App {...props} />
      </MemoryRouter>
    );
    expect(await screen.findByText(/FRED API key/i)).toBeInTheDocument();
  });
});
