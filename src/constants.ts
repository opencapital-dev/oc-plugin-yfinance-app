import pluginJson from './plugin.json';

export const PLUGIN_ID = pluginJson.id;
export const PLUGIN_BASE_URL = `/a/${pluginJson.id}`;

export enum ROUTES {
  Overview = 'overview',
  Instruments = 'instruments',
  Settings = 'settings',
}

export const PLUGIN_RESOURCES = `/api/plugins/${pluginJson.id}/resources`;
