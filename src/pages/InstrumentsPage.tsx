import React, { useState } from 'react';
import { css } from '@emotion/css';
import { type GrafanaTheme2 } from '@grafana/data';
import { TabsBar, Tab, TabContent, useStyles2 } from '@grafana/ui';

import { Page } from '../components/Page';
import { StocksTab } from '../components/instruments/StocksTab';
import { OptionsTab } from '../components/instruments/OptionsTab';

type TabKey = 'stocks' | 'options';

export function InstrumentsPage() {
  const [tab, setTab] = useState<TabKey>('stocks');
  const styles = useStyles2(getStyles);

  return (
    <Page>
      <Page.Contents>
        <TabsBar>
          <Tab
            label="Stocks"
            active={tab === 'stocks'}
            onChangeTab={() => setTab('stocks')}
          />
          <Tab
            label="Options"
            active={tab === 'options'}
            onChangeTab={() => setTab('options')}
          />
        </TabsBar>
        <TabContent className={styles.tabContent}>
          {tab === 'stocks' ? <StocksTab /> : <OptionsTab />}
        </TabContent>
      </Page.Contents>
    </Page>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  tabContent: css({
    paddingTop: theme.spacing(3),
  }),
});
