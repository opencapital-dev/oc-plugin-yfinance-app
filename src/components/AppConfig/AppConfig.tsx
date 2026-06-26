import React from 'react';
import { css } from '@emotion/css';
import { AppPluginMeta, GrafanaTheme2, PluginConfigPageProps } from '@grafana/data';
import { useStyles2 } from '@grafana/ui';

type AppPluginSettings = Record<string, never>;

export interface AppConfigProps extends PluginConfigPageProps<AppPluginMeta<AppPluginSettings>> {}

const AppConfig = (_props: AppConfigProps) => {
  const s = useStyles2(getStyles);
  return (
    <div className={s.wrapper}>
      <h3>yFinance Data</h3>
      <p className={s.body}>
        Read-only operator console wired to the <code>ingestor-yfinance</code> service.
        All configuration is service-side via environment variables:
      </p>
      <ul className={s.body}>
        <li>
          <code>YFINANCE_INGESTOR_URL</code> on the plugin&apos;s Go backend (default{' '}
          <code>http://ingestor-yfinance:8000</code>) — base URL for the upstream API.
        </li>
        <li>
          <code>POSTGRES_DSN</code>, <code>RISINGWAVE_HOST</code>,{' '}
          <code>RISINGWAVE_PORT</code> on the <code>ingestor-yfinance</code> container.
        </li>
      </ul>
      <p className={s.body}>
        Backfill jobs auto-enqueue from RisingWave subscriptions; the Jobs page is for
        observability and one-off manual re-runs.
      </p>
    </div>
  );
};

export default AppConfig;

const getStyles = (theme: GrafanaTheme2) => ({
  wrapper: css({
    display: 'flex',
    flexDirection: 'column',
    gap: theme.spacing(1),
    maxWidth: theme.spacing(80),
  }),
  body: css({
    color: theme.colors.text.secondary,
    fontSize: theme.typography.body.fontSize,
  }),
});
