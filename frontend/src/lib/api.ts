import axios from 'axios';
import type { AxiosError, InternalAxiosRequestConfig } from 'axios';
export const api = axios.create({ baseURL: '/api/v1', withCredentials: true });
api.interceptors.request.use(function configureCsrf(config) {
  const csrfCookie = document.cookie.split(';').map(function trimCookie(cookie) { return cookie.trim(); })
    .find(function findCsrf(cookie) { return cookie.startsWith('csrf_token='); });
  if (csrfCookie) {
    config.headers['X-CSRF-Token'] = decodeURIComponent(csrfCookie.slice('csrf_token='.length));
  }
  return config;
});

type RetryableRequestConfig = InternalAxiosRequestConfig & { _megopanelRetried?: boolean };
let refreshInFlight: Promise<void> | null = null;

api.interceptors.response.use(undefined, async function refreshExpiredSession(error: AxiosError) {
  const originalRequest = error.config as RetryableRequestConfig | undefined;
  const endpoint = originalRequest?.url || '';
  if (error.response?.status !== 401 || !originalRequest || originalRequest._megopanelRetried ||
      endpoint.includes('/auth/login') || endpoint.includes('/auth/refresh')) {
    return Promise.reject(error);
  }

  originalRequest._megopanelRetried = true;
  try {
    if (!refreshInFlight) {
      refreshInFlight = api.post('/auth/refresh').then(function refreshed() {}).finally(function clearRefreshPromise() {
        refreshInFlight = null;
      });
    }
    await refreshInFlight;
    return api.request(originalRequest);
  } catch {
    return Promise.reject(error);
  }
});
