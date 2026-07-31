import React from 'react';
import ReactDOM from 'react-dom/client';
import { QueryClient, QueryClientProvider, useMutation, useQuery } from '@tanstack/react-query';
import { BrowserRouter, Navigate, Route, Routes, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import './index.css';
import './lib/i18n';
import { api } from './lib/api';

type DashboardStats = {
  cpuUsage: number;
  ramUsage: number;
  diskUsage: number;
  uptime: string;
  osVersion: string;
  hostname: string;
};

type CardData = {
  label: string;
  value: string;
};

type Website = {
  id: number;
  domain: string;
  path: string;
  createdAt: string;
};

const queryClient = new QueryClient();

function LanguageSwitch(): React.JSX.Element {
  const { i18n } = useTranslation();

  function toggleLanguage(): void {
    const nextLanguage = i18n.language.startsWith('en') ? 'ru' : 'en';
    void i18n.changeLanguage(nextLanguage);
    localStorage.setItem('megopanel-language', nextLanguage);
  }

  return (
    <button className="btn" onClick={toggleLanguage} type="button">
      {i18n.language.toUpperCase()}
    </button>
  );
}

function Layout(properties: { children: React.ReactNode }): React.JSX.Element {
  return (
    <main className="min-h-screen p-4 md:p-8">
      <div className="mx-auto max-w-6xl">
        <header className="mb-8 flex items-center justify-between border-b border-white/5 pb-6">
          <div>
            <p className="text-xs font-bold uppercase tracking-widest text-purple-500">MegoPanel</p>
            <h1 className="text-3xl font-extrabold bg-clip-text text-transparent bg-gradient-to-r from-white via-slate-200 to-zinc-400">Server Management</h1>
          </div>
          <LanguageSwitch />
        </header>
        {properties.children}
      </div>
    </main>
  );
}

function Login(): React.JSX.Element {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const mutation = useMutation({
    mutationFn: function login(formData: FormData) {
      const username = formData.get('username');
      const password = formData.get('password');
      return api.post('/auth/login', { username: username, password: password });
    },
    onSuccess: function handleLoginSuccess(): void {
      navigate('/');
    },
  });

  function submitLogin(formData: FormData): void {
    mutation.mutate(formData);
  }

  return (
    <Layout>
      <form action={submitLogin} className="card mx-auto max-w-md space-y-6 p-8 bg-gradient-to-br from-purple-900/10 via-zinc-900/40 to-indigo-900/10">
        <div>
          <h2 className="text-2xl font-black bg-clip-text text-transparent bg-gradient-to-r from-white to-slate-400">{t('login')}</h2>
          <p className="text-xs text-slate-400 mt-1">Access your high-performance node</p>
        </div>
        <div className="space-y-4">
          <input className="input" name="username" placeholder={t('username')} required />
          <input className="input" name="password" placeholder={t('password')} type="password" required />
        </div>
        <button className="btn w-full" type="submit" disabled={mutation.isPending}>
          {mutation.isPending ? '...' : t('login')}
        </button>
        {mutation.error ? <p className="text-xs font-semibold text-rose-400 text-center">Login failed. Please verify your credentials.</p> : null}
      </form>
    </Layout>
  );
}

function Onboarding(): React.JSX.Element {
  const { t } = useTranslation();
  const [step, setStep] = React.useState(1);
  const [remoteAccessEnabled, setRemoteAccessEnabled] = React.useState(false);

  const adminMutation = useMutation({
    mutationFn: function createAdministrator(formData: FormData) {
      const username = formData.get('username');
      const password = formData.get('password');
      return api.post('/setup/admin', { username: username, password: password });
    },
    onSuccess: function handleAdministratorCreated(): void {
      setStep(2);
    },
  });

  const installMariaDbMutation = useMutation({
    mutationFn: function installMariaDb() {
      return api.post('/install/mariadb', { remoteAccess: remoteAccessEnabled });
    },
    onSuccess: function handleMariaDbInstalled(): void {
      setStep(3);
    },
  });

  const installNginxMutation = useMutation({
    mutationFn: function installNginx() {
      return api.post('/install/nginx');
    },
    onSuccess: function handleNginxInstalled(): void {
      window.location.href = '/';
    },
  });

  function submitAdministrator(formData: FormData): void {
    adminMutation.mutate(formData);
  }

  function changeRemoteAccess(event: React.ChangeEvent<HTMLInputElement>): void {
    setRemoteAccessEnabled(event.target.checked);
  }

  function installMariaDb(): void {
    installMariaDbMutation.mutate();
  }

  function installNginx(): void {
    installNginxMutation.mutate();
  }

  return (
    <Layout>
      <section className="card mx-auto max-w-2xl p-8 bg-gradient-to-br from-purple-900/10 via-zinc-900/40 to-indigo-900/10">
        <h2 className="mb-6 text-2xl font-extrabold bg-clip-text text-transparent bg-gradient-to-r from-white to-slate-400">{t('onboarding')}</h2>

        {step === 1 ? (
          <form action={submitAdministrator} className="space-y-4">
            <h3 className="text-lg font-bold text-purple-300">{t('createAdmin')}</h3>
            <input className="input" name="username" placeholder={t('username')} required />
            <input className="input" name="password" placeholder={t('password')} type="password" required />
            <input className="input" name="confirm" placeholder={t('confirm')} type="password" required />
            <button className="btn" type="submit" disabled={adminMutation.isPending}>
              {adminMutation.isPending ? '...' : t('next')}
            </button>
            {adminMutation.error ? <p className="text-xs font-semibold text-rose-400">Creation failed</p> : null}
          </form>
        ) : null}

        {step === 2 ? (
          <div className="space-y-4">
            <h3 className="text-xl font-bold text-purple-300">MariaDB</h3>
            <p className="text-sm text-slate-300">Status: <span className="font-semibold text-purple-400">{installMariaDbMutation.isSuccess ? t('installed') : t('notInstalled')}</span></p>
            <label className="flex items-center gap-3 text-sm text-slate-300 cursor-pointer select-none">
              <input className="accent-purple-600 rounded" checked={remoteAccessEnabled} onChange={changeRemoteAccess} type="checkbox" />
              {t('remote')}
            </label>
            <button className="btn" onClick={installMariaDb} type="button" disabled={installMariaDbMutation.isPending}>
              {installMariaDbMutation.isPending ? 'Installing...' : t('install')}
            </button>
            {installMariaDbMutation.error ? <p className="text-xs font-semibold text-rose-400">Installation failed</p> : null}
          </div>
        ) : null}

        {step === 3 ? (
          <div className="space-y-4">
            <h3 className="text-xl font-bold text-purple-300">Nginx</h3>
            <p className="text-sm text-slate-300">Status: <span className="font-semibold text-purple-400">{installNginxMutation.isSuccess ? t('installed') : t('notInstalled')}</span></p>
            <button className="btn" onClick={installNginx} type="button" disabled={installNginxMutation.isPending}>
              {installNginxMutation.isPending ? 'Installing...' : t('install')}
            </button>
            {installNginxMutation.error ? <p className="text-xs font-semibold text-rose-400">Installation failed</p> : null}
          </div>
        ) : null}
      </section>
    </Layout>
  );
}

function Dashboard(): React.JSX.Element {
  const { t } = useTranslation();

  const statsQuery = useQuery({
    queryKey: ['dashboard'],
    queryFn: async function loadDashboard(): Promise<DashboardStats> {
      const response = await api.get<DashboardStats>('/dashboard');
      return response.data;
    },
  });

  const websitesQuery = useQuery({
    queryKey: ['websites'],
    queryFn: async function loadWebsites(): Promise<Website[]> {
      const response = await api.get<Website[]>('/websites');
      return response.data;
    },
  });

  const createWebsiteMutation = useMutation({
    mutationFn: function createWebsite(data: { domain: string; path: string }) {
      return api.post('/websites', data);
    },
    onSuccess: function handleCreateSuccess() {
      void websitesQuery.refetch();
    },
  });

  const deleteWebsiteMutation = useMutation({
    mutationFn: function deleteWebsite(id: number) {
      return api.delete(`/websites/${id}`);
    },
    onSuccess: function handleDeleteSuccess() {
      void websitesQuery.refetch();
    },
  });

  const [domainInput, setDomainInput] = React.useState('');
  const [pathInput, setPathInput] = React.useState('');

  function handleCreateWebsite(event: React.FormEvent) {
    event.preventDefault();
    if (!domainInput || !pathInput) return;
    createWebsiteMutation.mutate({ domain: domainInput, path: pathInput }, {
      onSuccess: () => {
        setDomainInput('');
        setPathInput('');
      }
    });
  }

  const cards: CardData[] = statsQuery.data ? [
    { label: t('cpu'), value: statsQuery.data.cpuUsage + '%' },
    { label: t('ram'), value: statsQuery.data.ramUsage + '%' },
    { label: t('disk'), value: statsQuery.data.diskUsage + '%' },
    { label: t('uptime'), value: statsQuery.data.uptime },
    { label: t('os'), value: statsQuery.data.osVersion },
    { label: t('hostname'), value: statsQuery.data.hostname },
  ] : [];

  return (
    <Layout>
      {/* Stats Grid */}
      <div className="grid gap-6 md:grid-cols-3 mb-10">
        {cards.map(function renderCard(card: CardData): React.JSX.Element {
          return (
            <article className="card p-6 bg-gradient-to-br from-purple-900/10 via-zinc-900/40 to-indigo-900/10" key={card.label}>
              <p className="text-xs font-semibold tracking-wider uppercase text-purple-400">{card.label}</p>
              <p className="mt-3 text-3xl font-extrabold text-transparent bg-clip-text bg-gradient-to-r from-white via-slate-100 to-slate-300">{card.value}</p>
            </article>
          );
        })}
      </div>

      {/* Main Content Area */}
      <div className="grid gap-8 lg:grid-cols-3">
        {/* Left Column: Website Creation */}
        <section className="card p-6 lg:col-span-1 bg-gradient-to-br from-purple-900/5 via-zinc-900/40 to-indigo-900/5">
          <h3 className="text-xl font-bold mb-4 text-purple-300">{t('createSite')}</h3>
          <form onSubmit={handleCreateWebsite} className="space-y-4">
            <div>
              <label className="block text-xs font-semibold text-slate-400 mb-1 uppercase tracking-wider">{t('domain')}</label>
              <input
                className="input"
                value={domainInput}
                onChange={(e) => setDomainInput(e.target.value)}
                placeholder="example.com"
                required
              />
            </div>
            <div>
              <label className="block text-xs font-semibold text-slate-400 mb-1 uppercase tracking-wider">{t('path')}</label>
              <input
                className="input"
                value={pathInput}
                onChange={(e) => setPathInput(e.target.value)}
                placeholder="/var/www/example"
                required
              />
            </div>
            <button className="btn w-full mt-2" type="submit" disabled={createWebsiteMutation.isPending}>
              {createWebsiteMutation.isPending ? '...' : t('addSite')}
            </button>
          </form>
        </section>

        {/* Right Column: Websites List */}
        <section className="card p-6 lg:col-span-2 bg-gradient-to-br from-purple-900/5 via-zinc-900/40 to-indigo-900/5">
          <h3 className="text-xl font-bold mb-4 text-purple-300">{t('websites')}</h3>
          {websitesQuery.isLoading ? (
            <p className="text-sm text-slate-400">Loading websites...</p>
          ) : !websitesQuery.data || websitesQuery.data.length === 0 ? (
            <p className="text-sm text-slate-500 italic py-4">No websites configured yet.</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-left text-sm border-collapse">
                <thead>
                  <tr className="border-b border-white/5 text-slate-400 text-xs font-semibold uppercase tracking-wider">
                    <th className="py-3 px-4">{t('domain')}</th>
                    <th className="py-3 px-4">{t('path')}</th>
                    <th className="py-3 px-4 text-right"></th>
                  </tr>
                </thead>
                <tbody>
                  {websitesQuery.data.map((site) => (
                    <tr className="border-b border-white/5 hover:bg-white/5 transition duration-150" key={site.id}>
                      <td className="py-3 px-4 font-semibold text-slate-200">{site.domain}</td>
                      <td className="py-3 px-4 text-slate-400 font-mono text-xs">{site.path}</td>
                      <td className="py-3 px-4 text-right">
                        <button
                          className="btn-danger"
                          onClick={() => deleteWebsiteMutation.mutate(site.id)}
                          disabled={deleteWebsiteMutation.isPending}
                        >
                          {t('delete')}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      </div>
    </Layout>
  );
}

function Gate(): React.JSX.Element {
  const setupQuery = useQuery({
    queryKey: ['setup'],
    queryFn: async function loadSetupStatus(): Promise<{ configured: boolean }> {
      const response = await api.get<{ configured: boolean }>('/setup/status');
      return response.data;
    },
  });

  const currentUserQuery = useQuery({
    queryKey: ['me'],
    queryFn: async function loadCurrentUser() {
      const response = await api.get('/auth/me');
      return response.data;
    },
    retry: false,
    enabled: setupQuery.data?.configured === true,
  });

  if (setupQuery.isLoading) {
    return <Layout><p>Loading...</p></Layout>;
  }

  if (setupQuery.data?.configured === false) {
    return <Navigate to="/onboarding" />;
  }

  if (currentUserQuery.isError) {
    return <Navigate to="/login" />;
  }

  return <Dashboard />;
}

function App(): React.JSX.Element {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route element={<Gate />} path="/" />
          <Route element={<Login />} path="/login" />
          <Route element={<Onboarding />} path="/onboarding" />
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  );
}

const rootElement = document.getElementById('root') as HTMLElement;
ReactDOM.createRoot(rootElement).render(<App />);
