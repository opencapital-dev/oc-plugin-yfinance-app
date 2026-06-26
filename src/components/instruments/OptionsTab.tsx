import React, { useEffect, useState } from 'react';
import { css } from '@emotion/css';
import { type GrafanaTheme2 } from '@grafana/data';
import { Button, Input, Switch, useStyles2 } from '@grafana/ui';

import {
  listOptionUnderlyings,
  setOptionUnderlyingSymbol,
  toggleOptionUnderlying,
  type OptionUnderlying,
} from '../../api/options';

export function OptionsTab() {
  const styles = useStyles2(getStyles);
  const [rows, setRows] = useState<OptionUnderlying[]>([]);
  const [edits, setEdits] = useState<Record<string, string>>({});

  const load = () => listOptionUnderlyings().then(setRows);
  useEffect(() => {
    load();
  }, []);

  const key = (r: OptionUnderlying) => `${r.root}|${r.portfolio_id}`;

  return (
    <div className={styles.wrapper}>
      <table className={styles.table}>
        <thead>
          <tr>
            <th>Underlying</th>
            <th>Yahoo symbol</th>
            <th>Held contracts</th>
            <th>Subscribed</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={key(r)}>
              <td>
                <strong>{r.root}</strong>
              </td>
              <td>
                <Input
                  value={edits[key(r)] ?? r.symbol}
                  onChange={(e) => setEdits({ ...edits, [key(r)]: e.currentTarget.value })}
                  width={20}
                />
              </td>
              <td>{r.held_contracts}</td>
              <td>
                <Switch
                  value={r.subscribed}
                  onChange={async () => {
                    await toggleOptionUnderlying(r.root, r.portfolio_id, !r.subscribed);
                    load();
                  }}
                />
              </td>
              <td>
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={async () => {
                    await setOptionUnderlyingSymbol(r.root, r.portfolio_id, edits[key(r)] ?? r.symbol);
                    load();
                  }}
                >
                  Save
                </Button>
              </td>
            </tr>
          ))}
          {rows.length === 0 && (
            <tr>
              <td colSpan={5} className={styles.empty}>
                No option positions discovered yet.
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  wrapper: css({
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
  empty: css({
    padding: theme.spacing(4),
    textAlign: 'center',
    color: theme.colors.text.secondary,
    opacity: 0.7,
  }),
});
