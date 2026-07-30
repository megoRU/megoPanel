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

const queryClient = new QueryClient();

function LanguageSwitch(): React.JSX.Element {
  const { i18n } = useTranslation();

  function toggleLanguage(): void {
    const nextLanguage = i18n.language === 'en' ? 'ru' : 'en';
    void i18n.changeLanguage(nextLanguage);
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
        <header className="mb-8 flex items-center justify-between">
          <div>
            <p className="text-sm text-blue-300">MegoPanel</p>
            <h1 className="text-3xl font-bold">Server Management</h1>
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
      <form action={submitLogin} className="card mx-auto max-w-md space-y-4 p-8">
        <h2 className="text-2xl font-bold">{t('login')}</h2>
        <input className="input" name="username" placeholder={t('username')} />
        <input className="input" name="password" placeholder={t('password')} type="password" />
        <button className="btn w-full" type="submit">{t('login')}</button>
        {mutation.error ? <p className="text-red-300">Login failed</p> : null}
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
      <section className="card mx-auto max-w-2xl p-8">
        <h2 className="mb-6 text-2xl font-bold">{t('onboarding')}</h2>
        {step === 1 ? (
          <form action={submitAdministrator} className="space-y-4">
            <h3>{t('createAdmin')}</h3>
            <input className="input" name="username" placeholder={t('username')} />
            <input className="input" name="password" placeholder={t('password')} type="password" />
            <input className="input" name="confirm" placeholder={t('confirm')} type="password" />
            <button className="btn" type="submit">{t('next')}</button>
          </form>
        ) : null}
        {step === 2 ? (
          <div className="space-y-4">
            <h3 className="text-xl">MariaDB</h3>
            <p>Status: {installMariaDbMutation.isSuccess ? t('installed') : t('notInstalled')}</p>
            <label className="flex gap-3">
              <input checked={remoteAccessEnabled} onChange={changeRemoteAccess} type="checkbox" />
              {t('remote')}
            </label>
            <button className="btn" onClick={installMariaDb} type="button">{t('install')}</button>
          </div>
        ) : null}
        {step === 3 ? (
          <div className="space-y-4">
            <h3 className="text-xl">Nginx</h3>
            <p>Status: {installNginxMutation.isSuccess ? t('installed') : t('notInstalled')}</p>
            <button className="btn" onClick={installNginx} type="button">{t('install')}</button>
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
      <div className="grid gap-4 md:grid-cols-3">
        {cards.map(function renderCard(card: CardData): React.JSX.Element {
          return (
            <article className="card p-6" key={card.label}>
              <p className="text-sm text-slate-400">{card.label}</p>
              <p className="mt-3 text-2xl font-bold">{card.value}</p>
            </article>
          );
        })}
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
