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
  ramUsed: number;
  ramTotal: number;
  diskUsed: number;
  diskTotal: number;
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
  ipAddress: string;
  path: string;
  createdAt: string;
};

const queryClient = new QueryClient();

// Initialize theme from localStorage on module load
if (typeof window !== 'undefined') {
  const savedTheme = localStorage.getItem('megopanel-theme') || 'dark';
  document.documentElement.setAttribute('data-theme', savedTheme);
}

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

function ThemeSwitch(): React.JSX.Element {
  const { t } = useTranslation();
  const [theme, setTheme] = React.useState(() => {
    if (typeof window !== 'undefined') {
      return localStorage.getItem('megopanel-theme') || 'dark';
    }
    return 'dark';
  });

  function toggleTheme(): void {
    const nextTheme = theme === 'dark' ? 'light' : 'dark';
    setTheme(nextTheme);
    document.documentElement.setAttribute('data-theme', nextTheme);
    localStorage.setItem('megopanel-theme', nextTheme);
  }

  return (
    <button className="btn" onClick={toggleTheme} type="button">
      {theme === 'dark' ? t('dark').toUpperCase() : t('light').toUpperCase()}
    </button>
  );
}

function Layout(properties: { children: React.ReactNode }): React.JSX.Element {
  const { t } = useTranslation();
  return (
    <main className="min-h-screen p-4 md:p-8">
      <div className="mx-auto max-w-6xl">
        <header className="mb-8 flex items-center justify-between border-b border-[var(--border-color)] pb-6">
          <div>
            <p className="text-xs font-bold uppercase tracking-widest text-purple-500">MegoPanel</p>
            <h1 className="text-3xl font-extrabold bg-clip-text text-transparent bg-gradient-to-r from-[var(--title-gradient-from)] to-[var(--title-gradient-to)]">{t('serverManagement')}</h1>
          </div>
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
          <h2 className="text-2xl font-black bg-clip-text text-transparent bg-gradient-to-r from-[var(--title-gradient-from)] to-[var(--title-gradient-to)]">{t('login')}</h2>
          <p className="text-xs text-[var(--text-muted)] mt-1">Access your high-performance node</p>
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
      // Just keep step 2 and let user see success
    },
  });

  const installNginxMutation = useMutation({
    mutationFn: function installNginx() {
      return api.post('/install/nginx');
    },
    onSuccess: function handleNginxInstalled(): void {
      // Just keep step 2 and let user see success
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
        <h2 className="mb-6 text-2xl font-extrabold bg-clip-text text-transparent bg-gradient-to-r from-[var(--title-gradient-from)] to-[var(--title-gradient-to)]">{t('onboarding')}</h2>

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
          <div className="grid gap-6 md:grid-cols-2">
            {/* MariaDB Card */}
            <div className="space-y-4 rounded-2xl border border-[var(--border-color)] bg-black/10 p-5">
              <h3 className="text-xl font-bold text-purple-300">MariaDB</h3>
              <p className="text-sm text-[var(--text-color)]">Status: <span className="font-semibold text-purple-400">{installMariaDbMutation.isSuccess ? t('installed') : t('notInstalled')}</span></p>
              <label className="flex items-center gap-3 text-sm text-[var(--text-color)] cursor-pointer select-none">
                <input className="accent-purple-600 rounded" checked={remoteAccessEnabled} onChange={changeRemoteAccess} type="checkbox" />
                {t('remote')}
              </label>
              {!installMariaDbMutation.isSuccess && (
                <button
                  className="btn flex items-center justify-center gap-2 cursor-pointer w-full"
                  onClick={installMariaDb}
                  type="button"
                  disabled={installMariaDbMutation.isPending}
                >
                  {installMariaDbMutation.isPending ? (
                    <>
                      <svg className="animate-spin h-4 w-4 text-white" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                        <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                        <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                      </svg>
                      {t('installing')}
                    </>
                  ) : t('install')}
                </button>
              )}
              {installMariaDbMutation.error ? <p className="text-xs font-semibold text-rose-400">Installation failed</p> : null}
            </div>

            {/* Nginx Card */}
            <div className="space-y-4 rounded-2xl border border-[var(--border-color)] bg-black/10 p-5">
              <h3 className="text-xl font-bold text-purple-300">Nginx</h3>
              <p className="text-sm text-[var(--text-color)]">Status: <span className="font-semibold text-purple-400">{installNginxMutation.isSuccess ? t('installed') : t('notInstalled')}</span></p>
              {!installNginxMutation.isSuccess && (
                <button
                  className="btn flex items-center justify-center gap-2 cursor-pointer w-full"
                  onClick={installNginx}
                  type="button"
                  disabled={installNginxMutation.isPending}
                >
                  {installNginxMutation.isPending ? (
                    <>
                      <svg className="animate-spin h-4 w-4 text-white" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                        <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                        <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                      </svg>
                      {t('installing')}
                    </>
                  ) : t('install')}
                </button>
              )}
              {installNginxMutation.error ? <p className="text-xs font-semibold text-rose-400">Installation failed</p> : null}
            </div>

            <button className="btn md:col-span-2 cursor-pointer" onClick={function finishOnboarding(): void { window.location.href = '/'; }} type="button">
              {t('next')}
            </button>
          </div>
        ) : null}
      </section>
    </Layout>
  );
}

function Dashboard(): React.JSX.Element {
  const { t } = useTranslation();
  const [activeTab, setActiveTab] = React.useState<'websites' | 'databases' | 'settings'>('websites');

  const statsQuery = useQuery({
    queryKey: ['dashboard'],
    queryFn: async function loadDashboard(): Promise<DashboardStats> {
      const response = await api.get<DashboardStats>('/dashboard');
      return response.data;
    },
    refetchInterval: 1000,
  });

  const websitesQuery = useQuery({
    queryKey: ['websites'],
    queryFn: async function loadWebsites(): Promise<Website[]> {
      const response = await api.get<Website[]>('/websites');
      return response.data;
    },
  });

  const createWebsiteMutation = useMutation({
    mutationFn: function createWebsite(data: { domain: string; ipAddress: string }) {
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
  const [ipAddressInput, setIpAddressInput] = React.useState('');

  function handleCreateWebsite(event: React.FormEvent) {
    event.preventDefault();
    if (!domainInput || !ipAddressInput) {
      return;
    }
    createWebsiteMutation.mutate({ domain: domainInput, ipAddress: ipAddressInput }, {
      onSuccess: function resetWebsiteForm(): void {
        setDomainInput('');
        setIpAddressInput('');
      },
    });
  }

  // Database Management Queries and Mutations
  const mariadbStatusQuery = useQuery({
    queryKey: ['mariadb-status'],
    queryFn: async function loadMariaDbStatus() {
      const response = await api.get<{ name: string; installed: boolean }>('/install/mariadb/status');
      return response.data;
    },
  });

  const phpmyadminStatusQuery = useQuery({
    queryKey: ['phpmyadmin-status'],
    queryFn: async function loadPhpmyadminStatus() {
      const response = await api.get<{ name: string; installed: boolean }>('/install/phpmyadmin/status');
      return response.data;
    },
  });

  const databasesQuery = useQuery({
    queryKey: ['databases'],
    queryFn: async function loadDatabases(): Promise<string[]> {
      const response = await api.get<string[]>('/databases');
      return response.data;
    },
    enabled: mariadbStatusQuery.data?.installed === true,
  });

  const createDatabaseMutation = useMutation({
    mutationFn: function createDatabase(name: string) {
      return api.post('/databases', { name });
    },
    onSuccess: function handleCreateSuccess() {
      void databasesQuery.refetch();
    },
  });

  const deleteDatabaseMutation = useMutation({
    mutationFn: function deleteDatabase(name: string) {
      return api.delete(`/databases/${name}`);
    },
    onSuccess: function handleDeleteSuccess() {
      void databasesQuery.refetch();
    },
  });

  const installPhpmyadminMutation = useMutation({
    mutationFn: function installPhpmyadmin() {
      return api.post('/install/phpmyadmin');
    },
    onSuccess: function handleInstallSuccess() {
      void phpmyadminStatusQuery.refetch();
    },
  });

  const installMariaDbMutation = useMutation({
    mutationFn: function installMariaDb() {
      return api.post('/install/mariadb', { remoteAccess: false });
    },
    onSuccess: function handleInstallSuccess() {
      void mariadbStatusQuery.refetch();
    },
  });

  const [dbNameInput, setDbNameInput] = React.useState('');

  function handleCreateDatabase(event: React.FormEvent) {
    event.preventDefault();
    if (!dbNameInput) {
      return;
    }
    createDatabaseMutation.mutate(dbNameInput, {
      onSuccess: function resetDbForm(): void {
        setDbNameInput('');
      },
    });
  }

  const formatRAM = (stats: DashboardStats) => {
    if (stats.ramUsed !== undefined && stats.ramTotal !== undefined && stats.ramTotal > 0) {
      return stats.ramUsed.toFixed(2) + ' GB / ' + stats.ramTotal.toFixed(2) + ' GB';
    }
    return stats.ramUsage ? stats.ramUsage.toFixed(2) + '%' : '...';
  };

  const formatDisk = (stats: DashboardStats) => {
    if (stats.diskUsed !== undefined && stats.diskTotal !== undefined && stats.diskTotal > 0) {
      return stats.diskUsed.toFixed(2) + ' GB / ' + stats.diskTotal.toFixed(2) + ' GB';
    }
    return stats.diskUsage ? stats.diskUsage.toFixed(2) + '%' : '...';
  };

  const cards: CardData[] = statsQuery.data ? [
    { label: t('cpu'), value: statsQuery.data.cpuUsage.toFixed(2) + '%' },
    { label: t('ram'), value: formatRAM(statsQuery.data) },
    { label: t('disk'), value: formatDisk(statsQuery.data) },
  ] : [];

  return (
    <Layout>
      {/* Stats Grid */}
      <div className="grid gap-6 md:grid-cols-3 mb-10">
        {cards.map(function renderCard(card: CardData): React.JSX.Element {
          return (
            <article className="card p-6 bg-gradient-to-br from-purple-900/10 via-zinc-900/40 to-indigo-900/10" key={card.label}>
              <p className="text-xs font-semibold tracking-wider uppercase text-purple-400">{card.label}</p>
              <p className="mt-3 text-3xl font-extrabold text-transparent bg-clip-text bg-gradient-to-r from-[var(--title-gradient-from)] to-[var(--title-gradient-to)]">{card.value}</p>
            </article>
          );
        })}
      </div>

      {/* Navigation Tabs */}
      <div className="mb-8 border-b border-[var(--border-color)] flex gap-6">
        <button
          className={`pb-4 px-2 font-bold text-sm transition-all border-b-2 uppercase tracking-wider cursor-pointer ${
            activeTab === 'websites' ? 'border-purple-500 text-purple-400' : 'border-transparent text-[var(--text-muted)] hover:text-[var(--text-color)]'
          }`}
          onClick={function selectWebsitesTab() { setActiveTab('websites'); }}
          type="button"
        >
          {t('websites')}
        </button>
        <button
          className={`pb-4 px-2 font-bold text-sm transition-all border-b-2 uppercase tracking-wider cursor-pointer ${
            activeTab === 'databases' ? 'border-purple-500 text-purple-400' : 'border-transparent text-[var(--text-muted)] hover:text-[var(--text-color)]'
          }`}
          onClick={function selectDatabasesTab() { setActiveTab('databases'); }}
          type="button"
        >
          {t('databases')}
        </button>
        <button
          className={`pb-4 px-2 font-bold text-sm transition-all border-b-2 uppercase tracking-wider cursor-pointer ${
            activeTab === 'settings' ? 'border-purple-500 text-purple-400' : 'border-transparent text-[var(--text-muted)] hover:text-[var(--text-color)]'
          }`}
          onClick={function selectSettingsTab() { setActiveTab('settings'); }}
          type="button"
        >
          {t('settings')}
        </button>
      </div>

      {/* Tab Contents */}
      {activeTab === 'websites' ? (
        <div className="grid gap-8 lg:grid-cols-3 animate-fadeIn">
          {/* Create Website Form */}
          <section className="card p-6 lg:col-span-1 bg-gradient-to-br from-purple-900/5 via-zinc-900/40 to-indigo-900/5 h-fit">
            <h3 className="text-xl font-bold mb-4 text-purple-300">{t('createSite')}</h3>
            <form onSubmit={handleCreateWebsite} className="space-y-4">
              <div>
                <label className="block text-xs font-semibold text-[var(--text-muted)] mb-1 uppercase tracking-wider">{t('domain')}</label>
                <input
                  className="input"
                  value={domainInput}
                  onChange={function handleDomainInputChange(event: React.ChangeEvent<HTMLInputElement>): void { setDomainInput(event.target.value); }}
                  placeholder="example.com"
                  required
                />
              </div>
              <div>
                <label className="block text-xs font-semibold text-[var(--text-muted)] mb-1 uppercase tracking-wider">{t('ipAddress')}</label>
                <input
                  className="input"
                  value={ipAddressInput}
                  onChange={function handleIpAddressInputChange(event: React.ChangeEvent<HTMLInputElement>): void { setIpAddressInput(event.target.value); }}
                  placeholder="192.0.2.10"
                  required
                />
              </div>
              {createWebsiteMutation.isError ? (
                <p className="text-xs font-semibold text-rose-400 mt-2">
                  {(createWebsiteMutation.error as any)?.response?.data?.error || t('siteExistsError')}
                </p>
              ) : null}
              {createWebsiteMutation.isSuccess ? (
                <p className="text-xs font-semibold text-emerald-400 mt-2">{t('siteAdded')}</p>
              ) : null}
              <button className="btn w-full mt-2 cursor-pointer" type="submit" disabled={createWebsiteMutation.isPending}>
                {createWebsiteMutation.isPending ? '...' : t('addSite')}
              </button>
            </form>
          </section>

          {/* Websites List */}
          <section className="card p-6 lg:col-span-2 bg-gradient-to-br from-purple-900/5 via-zinc-900/40 to-indigo-900/5">
            <h3 className="text-xl font-bold mb-4 text-purple-300">{t('websites')}</h3>
            {websitesQuery.isLoading ? (
              <p className="text-sm text-[var(--text-muted)]">Loading websites...</p>
            ) : !websitesQuery.data || websitesQuery.data.length === 0 ? (
              <p className="text-sm text-[var(--text-muted)] italic py-4">{t('noWebsites')}</p>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-left text-sm border-collapse">
                  <thead>
                    <tr className="border-b border-[var(--border-color)] text-[var(--table-header)] text-xs font-semibold uppercase tracking-wider">
                      <th className="py-3 px-4">{t('domain')}</th>
                      <th className="py-3 px-4">{t('ipAddress')}</th>
                      <th className="py-3 px-4">{t('path')}</th>
                      <th className="py-3 px-4 text-right"></th>
                    </tr>
                  </thead>
                  <tbody>
                    {websitesQuery.data.map(function renderWebsite(site: Website): React.JSX.Element {
                      return (
                        <tr className="border-b border-[var(--border-color)] hover:bg-[var(--table-hover)] transition duration-150" key={site.id}>
                          <td className="py-3 px-4 font-semibold text-[var(--text-color)]">{site.domain}</td>
                          <td className="py-3 px-4 text-[var(--text-muted)] font-mono text-xs">{site.ipAddress}</td>
                          <td className="py-3 px-4 text-[var(--text-muted)] font-mono text-xs">{site.path}</td>
                          <td className="py-3 px-4 text-right">
                            <button
                              className="btn-danger cursor-pointer"
                              onClick={function deleteSelectedWebsite(): void { deleteWebsiteMutation.mutate(site.id); }}
                              disabled={deleteWebsiteMutation.isPending}
                            >
                              {t('delete')}
                            </button>
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            )}
          </section>
        </div>
      ) : null}

      {activeTab === 'databases' ? (
        <div className="space-y-8 animate-fadeIn">
          {mariadbStatusQuery.isLoading ? (
            <p className="text-sm text-[var(--text-muted)]">Loading database status...</p>
          ) : !mariadbStatusQuery.data?.installed ? (
            <div className="card p-8 bg-gradient-to-br from-purple-900/10 via-zinc-900/40 to-indigo-900/10 text-center max-w-xl mx-auto space-y-4">
              <h3 className="text-xl font-bold text-purple-300">MariaDB</h3>
              <p className="text-[var(--text-color)] text-sm">{t('mariadbNotInstalled')}</p>
              <button
                className="btn cursor-pointer"
                onClick={function installDbService() { installMariaDbMutation.mutate(); }}
                disabled={installMariaDbMutation.isPending}
              >
                {installMariaDbMutation.isPending ? t('installing') : t('installMariaDb')}
              </button>
            </div>
          ) : (
            <div className="space-y-8">
              <div className="grid gap-8 lg:grid-cols-3">
                {/* Create Database Form */}
                <section className="card p-6 lg:col-span-1 bg-gradient-to-br from-purple-900/5 via-zinc-900/40 to-indigo-900/5 h-fit">
                  <h3 className="text-xl font-bold mb-4 text-purple-300">{t('createDb')}</h3>
                  <form onSubmit={handleCreateDatabase} className="space-y-4">
                    <div>
                      <label className="block text-xs font-semibold text-[var(--text-muted)] mb-1 uppercase tracking-wider">{t('dbName')}</label>
                      <input
                        className="input"
                        value={dbNameInput}
                        onChange={function handleDbNameInputChange(event: React.ChangeEvent<HTMLInputElement>): void { setDbNameInput(event.target.value); }}
                        placeholder="my_database"
                        required
                      />
                    </div>
                    {createDatabaseMutation.isError ? (
                      <p className="text-xs font-semibold text-rose-400 mt-2">
                        {(createDatabaseMutation.error as any)?.response?.data?.error || t('dbExistsError')}
                      </p>
                    ) : null}
                    {createDatabaseMutation.isSuccess ? (
                      <p className="text-xs font-semibold text-emerald-400 mt-2">{t('dbCreated')}</p>
                    ) : null}
                    <button className="btn w-full mt-2 cursor-pointer" type="submit" disabled={createDatabaseMutation.isPending}>
                      {createDatabaseMutation.isPending ? '...' : t('addDb')}
                    </button>
                  </form>
                </section>

                {/* Databases List */}
                <section className="card p-6 lg:col-span-2 bg-gradient-to-br from-purple-900/5 via-zinc-900/40 to-indigo-900/5">
                  <h3 className="text-xl font-bold mb-4 text-purple-300">{t('databases')}</h3>
                  {databasesQuery.isLoading ? (
                    <p className="text-sm text-[var(--text-muted)]">Loading databases...</p>
                  ) : !databasesQuery.data || databasesQuery.data.length === 0 ? (
                    <p className="text-sm text-[var(--text-muted)] italic py-4">{t('noDatabases')}</p>
                  ) : (
                    <div className="overflow-x-auto">
                      <table className="w-full text-left text-sm border-collapse">
                        <thead>
                          <tr className="border-b border-[var(--border-color)] text-[var(--table-header)] text-xs font-semibold uppercase tracking-wider">
                            <th className="py-3 px-4">{t('dbName')}</th>
                            <th className="py-3 px-4 text-right"></th>
                          </tr>
                        </thead>
                        <tbody>
                          {databasesQuery.data.map(function renderDatabase(db: string): React.JSX.Element {
                            return (
                              <tr className="border-b border-[var(--border-color)] hover:bg-[var(--table-hover)] transition duration-150" key={db}>
                                <td className="py-3 px-4 font-semibold text-[var(--text-color)]">{db}</td>
                                <td className="py-3 px-4 text-right">
                                  <button
                                    className="btn-danger cursor-pointer"
                                    onClick={function deleteSelectedDatabase(): void { deleteDatabaseMutation.mutate(db); }}
                                    disabled={deleteDatabaseMutation.isPending}
                                  >
                                    {t('delete')}
                                  </button>
                                </td>
                              </tr>
                            );
                          })}
                        </tbody>
                      </table>
                    </div>
                  )}
                </section>
              </div>

              {/* phpMyAdmin Panel */}
              <section className="card p-6 bg-gradient-to-br from-purple-900/5 via-zinc-900/40 to-indigo-900/5">
                <h3 className="text-xl font-bold mb-4 text-purple-300">{t('phpMyAdminStatus')}</h3>
                {phpmyadminStatusQuery.isLoading ? (
                  <p className="text-sm text-[var(--text-muted)]">Loading phpMyAdmin status...</p>
                ) : phpmyadminStatusQuery.data?.installed ? (
                  <div className="flex flex-col md:flex-row items-start md:items-center justify-between gap-4">
                    <p className="text-[var(--text-color)] text-sm">{t('phpMyAdminInstalled')}</p>
                    <button
                      className="btn cursor-pointer"
                      onClick={function openPma() {
                        window.open(window.location.protocol + '//' + window.location.hostname + ':8080', '_blank');
                      }}
                    >
                      {t('openPhpMyAdmin')}
                    </button>
                  </div>
                ) : (
                  <div className="flex flex-col md:flex-row items-start md:items-center justify-between gap-4">
                    <p className="text-[var(--text-color)] text-sm">{t('phpMyAdminNotInstalled')}</p>
                    <button
                      className="btn cursor-pointer"
                      onClick={function installPma() { installPhpmyadminMutation.mutate(); }}
                      disabled={installPhpmyadminMutation.isPending}
                    >
                      {installPhpmyadminMutation.isPending ? t('pmaInstalling') : t('installPhpMyAdmin')}
                    </button>
                  </div>
                )}
              </section>
            </div>
          )}
        </div>
      ) : null}

      {activeTab === 'settings' ? (
        <div className="grid gap-8 lg:grid-cols-2 animate-fadeIn">
          <section className="card p-6 bg-gradient-to-br from-purple-900/5 via-zinc-900/40 to-indigo-900/5">
            <h3 className="text-xl font-bold mb-4 text-purple-300">{t('settings')}</h3>

            {/* Language Setting */}
            <div className="flex items-center justify-between py-4 border-b border-[var(--border-color)]">
              <div>
                <p className="font-semibold text-[var(--text-color)]">{t('language')}</p>
                <p className="text-xs text-[var(--text-muted)]">{t('changeLanguage')}</p>
              </div>
              <LanguageSwitch />
            </div>

            {/* Theme Setting */}
            <div className="flex items-center justify-between py-4">
              <div>
                <p className="font-semibold text-[var(--text-color)]">{t('theme')}</p>
                <p className="text-xs text-[var(--text-muted)]">Switch between light and dark themes</p>
              </div>
              <ThemeSwitch />
            </div>
          </section>
        </div>
      ) : null}
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
