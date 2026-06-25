import React from 'react';
import { Navigate, Route, Routes } from 'react-router-dom';
import { type AppRootProps } from '@grafana/data';

import { ROUTES } from '../../constants';
import { OverviewPage } from '../../pages/OverviewPage';
import { TickersPage } from '../../pages/TickersPage';
import { SettingsPage } from '../../pages/SettingsPage';

function App(_props: AppRootProps) {
  return (
    <Routes>
      <Route path={ROUTES.Overview} element={<OverviewPage />} />
      <Route path={ROUTES.Instruments} element={<TickersPage />} />
      <Route path={ROUTES.Settings} element={<SettingsPage />} />
      <Route path="*" element={<Navigate replace to={ROUTES.Overview} />} />
    </Routes>
  );
}

export default App;
