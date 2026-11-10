// Vite exposes env vars to client code via import.meta.env
export const apiUrl = import.meta.env.VITE_API_URL;
export const analyticsId = import.meta.env["VITE_ANALYTICS_ID"];
export const isProd = import.meta.env.PROD; // built-in Vite flag, not a user var
export const { VITE_FEATURE_X, VITE_THEME: theme } = import.meta.env;
