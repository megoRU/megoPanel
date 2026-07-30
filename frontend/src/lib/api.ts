import axios from 'axios';
export const api = axios.create({ baseURL: '/api/v1', withCredentials: true });
api.interceptors.request.use(function configureCsrf(config) { config.headers['X-CSRF-Token'] = 'megopanel-csrf'; return config; });
