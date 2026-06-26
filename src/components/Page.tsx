import React, { type ReactNode } from 'react';
import { css } from '@emotion/css';
import { type GrafanaTheme2 } from '@grafana/data';
import { useStyles2 } from '@grafana/ui';
import { PluginPage } from '@grafana/runtime';

type Props = {
  children?: ReactNode;
  /** Accepted for source-compat with Grafana core <Page>; unused inside plugin. */
  navId?: string;
  pageNav?: { text?: string; active?: boolean };
  layout?: unknown;
};

export function Page({ children }: Props) {
  return <PluginPage>{children}</PluginPage>;
}

function Contents({ children }: { children?: ReactNode }) {
  const s = useStyles2(getContentStyles);
  return <div className={s.wrap}>{children}</div>;
}

const getContentStyles = (theme: GrafanaTheme2) => ({
  wrap: css({
    padding: theme.spacing(3),
    maxWidth: 960,
    margin: '0 auto',
    width: '100%',
    display: 'flex',
    flexDirection: 'column',
    gap: theme.spacing(2),
  }),
});

Page.Contents = Contents;
