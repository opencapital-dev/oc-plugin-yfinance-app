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
        Read-only operator console for the <code>basic-data-app</code> plugin. The
        backend runs in-process and talks directly to the data plane; configuration
        is supplied by the host application through Grafana app provisioning (jsonData):
      </p>
      <ul className={s.body}>
        <li>
          <code>basic-data-app_url</code> — base URL the plugin backend uses to reach
          the data plane.
        </li>
        <li>
          <code>RISINGWAVE_HOST</code>, <code>RISINGWAVE_PORT</code> and the Postgres
          connection coordinates — the two-store data plane the plugin reads and writes.
        </li>
      </ul>
      <p className={s.body}>
        Backfill jobs auto-enqueue from instrument discovery; the Tickers page is for
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
