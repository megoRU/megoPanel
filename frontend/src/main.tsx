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

type UpdateStatusResponse = {
  currentVersion: string;
  latestVersion: string;
  hasUpdate: boolean;
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

type BackupRecord = {
  id: string;
  createdAt: string;
  appVersion: string;
  components: string[];
  sizeBytes: number;
  verified: boolean;
};

type BackupSettings = {
  directory: string;
  scheduleEnabled: boolean;
  scheduleHour: number;
  retentionCount: number;
  retentionDays: number;
  scheduleComponents: string[];
};

type BackupOperation = {
  at: string;
  action: string;
  status: string;
  message: string;
};

const queryClient = new QueryClient();

// Initialize theme from localStorage on module load
if (typeof window !== 'undefined') {
  const savedTheme = localStorage.getItem('megopanel-theme') || 'dark';
  document.documentElement.setAttribute('data-theme', savedTheme);
}

function UpdateComponent(): React.JSX.Element {
  const { t } = useTranslation();
  const [isChecking, setIsChecking] = React.useState(false);
  const [updateStatus, setUpdateStatus] = React.useState<UpdateStatusResponse | null>(null);
  const [installError, setInstallError] = React.useState<string | null>(null);

  const upgradeMutation = useMutation({
    mutationFn: function upgradeApplication() {
      return api.post('/update/upgrade');
    },
    onSuccess: function handleUpgradeSuccess() {
      // Re-trigger checking or set state
      setInstallError(null);
    },
    onError: function handleUpgradeError(err: any) {
      const errMsg = err?.response?.data?.error || t('updateFailed');
      setInstallError(errMsg);
    }
  });

  function checkUpdates() {
    setIsChecking(true);
    setInstallError(null);
    api.get<UpdateStatusResponse>('/update/status')
      .then(res => {
        setUpdateStatus(res.data);
      })
      .catch(err => {
        console.error('Check update failed:', err);
      })
      .finally(() => {
        setIsChecking(false);
      });
  }

  return (
    <div className="space-y-4 pt-2">
      {updateStatus && (
        <div className="space-y-2">
          <p className="text-sm text-[var(--text-color)]">
            <span className="font-semibold text-[var(--text-muted)] mr-2">{t('updateLatestVersion')}</span>
            <span className="font-mono text-zinc-400">{updateStatus.latestVersion}</span>
          </p>
          {updateStatus.hasUpdate ? (
            <div className="p-3 bg-amber-500/10 text-amber-400 border border-amber-500/20 rounded-md text-xs">
              {t('updateNewAvailable')} <span className="font-bold">{updateStatus.latestVersion}</span>
            </div>
          ) : (
            <div className="p-3 bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 rounded-md text-xs">
              {t('updateLatest')}
            </div>
          )}
        </div>
      )}

      {installError && (
        <div className="p-3 bg-rose-500/10 text-rose-400 border border-rose-500/20 rounded-md text-xs">
          {installError}
        </div>
      )}

      {upgradeMutation.isSuccess && (
        <div className="p-3 bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 rounded-md text-xs">
          {t('updateInstalling')}
        </div>
      )}

      <div className="flex gap-3 pt-2">
        <button
          className="btn-secondary text-sm px-4 py-2 cursor-pointer"
          onClick={checkUpdates}
          disabled={isChecking || upgradeMutation.isPending}
          type="button"
        >
          {isChecking ? t('updateChecking') : t('updateCheck')}
        </button>

        {updateStatus?.hasUpdate && !upgradeMutation.isSuccess && (
          <button
            className="btn text-sm px-4 py-2 cursor-pointer"
            onClick={() => upgradeMutation.mutate()}
            disabled={upgradeMutation.isPending}
            type="button"
          >
            {upgradeMutation.isPending ? t('updateInstalling') : t('updateInstall')}
          </button>
        )}
      </div>
    </div>
  );
}

function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return '—';
  if (bytes < 1024) return `${bytes} B`;
  const units = ['KB', 'MB', 'GB', 'TB'];
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value.toFixed(1)} ${units[unit]}`;
}

function apiErrorMessage(error: unknown, fallback: string): string {
  return (error as any)?.response?.data?.error || fallback;
}

function BackupsPanel(): React.JSX.Element {
  const { t, i18n } = useTranslation();
  const [selectedComponents, setSelectedComponents] = React.useState<string[]>(['panel', 'mariadb']);
  const [settingsForm, setSettingsForm] = React.useState<BackupSettings>({
    directory: '/var/lib/megopanel/backups', scheduleEnabled: false, scheduleHour: 3, retentionCount: 10, retentionDays: 30, scheduleComponents: ['panel', 'mariadb', 'websites'],
  });
  const [notice, setNotice] = React.useState<string | null>(null);
  const [verifiedIds, setVerifiedIds] = React.useState<string[]>([]);

  const settingsQuery = useQuery({
    queryKey: ['backup-settings'],
    queryFn: async function loadBackupSettings(): Promise<BackupSettings> {
      const response = await api.get<BackupSettings>('/backups/settings');
      return response.data;
    },
  });
  const backupsQuery = useQuery({
    queryKey: ['backups'],
    queryFn: async function loadBackups(): Promise<BackupRecord[]> {
      const response = await api.get<BackupRecord[]>('/backups');
      return response.data;
    },
    refetchInterval: 15000,
  });
  const operationsQuery = useQuery({
    queryKey: ['backup-operations'],
    queryFn: async function loadBackupOperations(): Promise<BackupOperation[]> {
      const response = await api.get<BackupOperation[]>('/backups/operations');
      return response.data;
    },
    refetchInterval: 15000,
  });

  React.useEffect(function syncSettingsForm() {
    if (settingsQuery.data) setSettingsForm(settingsQuery.data);
  }, [settingsQuery.data]);

  const refreshBackups = function refreshBackupQueries() {
    void backupsQuery.refetch();
    void operationsQuery.refetch();
  };
  const createMutation = useMutation({
    mutationFn: function createBackup() {
      return api.post<BackupRecord>('/backups', { components: selectedComponents });
    },
    onSuccess: function backupCreated(response) {
      setNotice(`${t('backupCreated')} ${response.data.id}`);
      refreshBackups();
    },
    onError: function backupCreationFailed(error: unknown) {
      setNotice(apiErrorMessage(error, t('backupFailed')));
      refreshBackups();
    },
  });
  const settingsMutation = useMutation({
    mutationFn: function saveBackupSettings() {
      return api.put<BackupSettings>('/backups/settings', settingsForm);
    },
    onSuccess: function backupSettingsSaved(response) {
      setSettingsForm(response.data);
      setNotice(t('backupSettingsSaved'));
      void settingsQuery.refetch();
    },
    onError: function backupSettingsFailed(error: unknown) {
      setNotice(apiErrorMessage(error, t('backupFailed')));
    },
  });
  const verifyMutation = useMutation({
    mutationFn: function verifyBackup(id: string) {
      return api.post<BackupRecord>(`/backups/${id}/verify`);
    },
    onSuccess: function backupVerified(response) {
      setVerifiedIds(function addVerifiedId(ids) { return ids.includes(response.data.id) ? ids : [...ids, response.data.id]; });
      setNotice(t('backupVerified'));
      refreshBackups();
    },
    onError: function backupVerificationFailed(error: unknown) {
      setNotice(apiErrorMessage(error, t('backupFailed')));
      refreshBackups();
    },
  });
  const deleteMutation = useMutation({
    mutationFn: function deleteBackup(id: string) {
      return api.delete(`/backups/${id}`);
    },
    onSuccess: function backupDeleted() {
      setNotice(t('backupDeleted'));
      refreshBackups();
    },
    onError: function backupDeletionFailed(error: unknown) {
      setNotice(apiErrorMessage(error, t('backupFailed')));
      refreshBackups();
    },
  });
  const restoreMutation = useMutation({
    mutationFn: function restoreBackup(id: string) {
      return api.post<{ preRestoreBackupId: string; restartScheduled: boolean }>(`/backups/${id}/restore`, { confirm: true });
    },
    onSuccess: function backupRestored(response) {
      const restart = response.data.restartScheduled ? t('backupRestartScheduled') : t('backupRestartManual');
      setNotice(`${t('backupRestoreComplete')} ${response.data.preRestoreBackupId}. ${restart}`);
      refreshBackups();
    },
    onError: function backupRestoreFailed(error: unknown) {
      setNotice(apiErrorMessage(error, t('backupFailed')));
      refreshBackups();
    },
  });

  const busy = createMutation.isPending || settingsMutation.isPending || verifyMutation.isPending || deleteMutation.isPending || restoreMutation.isPending;
  const error = settingsQuery.error || backupsQuery.error || operationsQuery.error;

  function toggleComponent(component: string): void {
    setSelectedComponents(function updateSelected(current) {
      return current.includes(component) ? current.filter(function omit(value) { return value !== component; }) : [...current, component];
    });
  }

  async function downloadBackup(id: string): Promise<void> {
    try {
      const response = await api.get(`/backups/${id}/download`, { responseType: 'blob' });
      const url = URL.createObjectURL(response.data);
      const anchor = document.createElement('a');
      anchor.href = url;
      anchor.download = `${id}.zip`;
      document.body.appendChild(anchor);
      anchor.click();
      anchor.remove();
      URL.revokeObjectURL(url);
    } catch (downloadError) {
      setNotice(apiErrorMessage(downloadError, t('backupFailed')));
    }
  }

  return (
    <div className="space-y-6 animate-fadeIn">
      <section className="card p-6 space-y-5">
        <div>
          <h3 className="text-base font-semibold text-[var(--heading-color)]">{t('backupCreateTitle')}</h3>
          <p className="text-sm text-[var(--text-muted)] mt-1">{t('backupCreateDescription')}</p>
        </div>
        <div className="flex flex-wrap gap-x-6 gap-y-3 text-sm">
          {(['panel', 'mariadb', 'websites'] as const).map(function renderComponent(component) {
            const label = component === 'panel' ? t('backupPanel') : component === 'mariadb' ? t('backupMariaDb') : t('backupWebsites');
            return (
              <label className="inline-flex items-center gap-2" key={component}>
                <input type="checkbox" checked={selectedComponents.includes(component)} disabled={busy} onChange={function changeComponent() { toggleComponent(component); }} />
                <span>{label}</span>
              </label>
            );
          })}
        </div>
        <button className="btn px-4 py-2" type="button" disabled={busy || selectedComponents.length === 0} onClick={function startBackup() { setNotice(null); createMutation.mutate(); }}>
          {createMutation.isPending ? t('backupCreating') : t('backupCreate')}
        </button>
      </section>

      <section className="card p-6 space-y-4">
        <h3 className="text-base font-semibold text-[var(--heading-color)]">{t('backupSettingsTitle')}</h3>
        <div className="grid gap-4 md:grid-cols-2">
          <label className="space-y-1 text-xs font-medium text-[var(--text-muted)]">
            <span>{t('backupDirectory')}</span>
            <input className="input" value={settingsForm.directory} disabled={busy} onChange={function changeDirectory(event) { setSettingsForm({ ...settingsForm, directory: event.target.value }); }} />
          </label>
          <label className="inline-flex items-center gap-2 text-sm self-end pb-2">
            <input type="checkbox" checked={settingsForm.scheduleEnabled} disabled={busy} onChange={function changeSchedule() { setSettingsForm({ ...settingsForm, scheduleEnabled: !settingsForm.scheduleEnabled }); }} />
            <span>{t('backupScheduleEnabled')}</span>
          </label>
          <fieldset className="space-y-2 text-sm md:col-span-2" disabled={busy}>
            <legend className="mb-2 text-xs font-medium text-[var(--text-muted)]">{t('backupScheduleComponents')}</legend>
            <div className="flex flex-wrap gap-x-6 gap-y-2">
              {(['panel', 'mariadb', 'websites'] as const).map(function renderScheduledComponent(component) {
                const label = component === 'panel' ? t('backupPanel') : component === 'mariadb' ? t('backupMariaDb') : t('backupWebsites');
                return <label className="inline-flex items-center gap-2" key={component}>
                  <input type="checkbox" checked={settingsForm.scheduleComponents.includes(component)} onChange={function toggleScheduledComponent() {
                    const components = settingsForm.scheduleComponents.includes(component)
                      ? settingsForm.scheduleComponents.filter(function omit(value) { return value !== component; })
                      : [...settingsForm.scheduleComponents, component];
                    setSettingsForm({ ...settingsForm, scheduleComponents: components });
                  }} />
                  <span>{label}</span>
                </label>;
              })}
            </div>
          </fieldset>
          <label className="space-y-1 text-xs font-medium text-[var(--text-muted)]">
            <span>{t('backupScheduleHour')}</span>
            <input className="input" type="number" min={0} max={23} value={settingsForm.scheduleHour} disabled={busy} onChange={function changeHour(event) { setSettingsForm({ ...settingsForm, scheduleHour: Number(event.target.value) }); }} />
          </label>
          <label className="space-y-1 text-xs font-medium text-[var(--text-muted)]">
            <span>{t('backupRetentionCount')}</span>
            <input className="input" type="number" min={1} max={1000} value={settingsForm.retentionCount} disabled={busy} onChange={function changeCount(event) { setSettingsForm({ ...settingsForm, retentionCount: Number(event.target.value) }); }} />
          </label>
          <label className="space-y-1 text-xs font-medium text-[var(--text-muted)]">
            <span>{t('backupRetentionDays')}</span>
            <input className="input" type="number" min={1} max={3650} value={settingsForm.retentionDays} disabled={busy} onChange={function changeDays(event) { setSettingsForm({ ...settingsForm, retentionDays: Number(event.target.value) }); }} />
          </label>
        </div>
        <p className="text-xs text-amber-500">{t('backupSameDiskWarning')}</p>
        <button className="btn-secondary px-4 py-2" type="button" disabled={busy || settingsQuery.isLoading} onClick={function saveSettings() { settingsMutation.mutate(); }}>
          {settingsMutation.isPending ? t('loading') : t('backupSaveSettings')}
        </button>
      </section>

      {notice ? <div className="rounded-md border border-[var(--border-color)] px-4 py-3 text-sm text-[var(--text-color)]" role="status">{notice}</div> : null}
      {error ? <div className="rounded-md border border-rose-500/30 bg-rose-500/10 px-4 py-3 text-sm text-rose-500" role="alert">{apiErrorMessage(error, t('backupFailed'))}</div> : null}

      <section className="card p-6 space-y-4">
        <div className="flex items-center justify-between gap-4">
          <h3 className="text-base font-semibold text-[var(--heading-color)]">{t('backupListTitle')}</h3>
          <button className="btn-secondary px-3 py-1.5 text-xs" type="button" disabled={backupsQuery.isFetching} onClick={function reloadBackups() { refreshBackups(); }}>{t('backupRefresh')}</button>
        </div>
        {backupsQuery.isLoading ? <p className="text-sm text-[var(--text-muted)]">{t('loading')}</p> : null}
        {!backupsQuery.isLoading && backupsQuery.data?.length === 0 ? <p className="text-sm text-[var(--text-muted)]">{t('backupEmpty')}</p> : null}
        {backupsQuery.data && backupsQuery.data.length > 0 ? (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[760px] text-left text-sm">
              <thead className="text-xs uppercase tracking-wider text-[var(--text-muted)]">
                <tr><th className="py-3 pr-4">{t('backupDate')}</th><th className="py-3 pr-4">{t('backupComponents')}</th><th className="py-3 pr-4">{t('backupSize')}</th><th className="py-3 pr-4">{t('backupStatus')}</th><th className="py-3">{t('backupActions')}</th></tr>
              </thead>
              <tbody>
                {backupsQuery.data.map(function renderBackup(backup) {
                  const components = backup.components.map(function translateComponent(component) { return component === 'panel' ? t('backupPanel') : component === 'mariadb' ? t('backupMariaDb') : t('backupWebsites'); }).join(', ');
                  const verified = backup.verified || verifiedIds.includes(backup.id);
                  return (
                    <tr className="border-t border-[var(--border-color)] align-top" key={backup.id}>
                      <td className="py-3 pr-4 whitespace-nowrap">{new Date(backup.createdAt).toLocaleString(i18n.language)}</td>
                      <td className="py-3 pr-4">{components}</td>
                      <td className="py-3 pr-4 whitespace-nowrap">{formatBytes(backup.sizeBytes)}</td>
                      <td className="py-3 pr-4">{verified ? <span className="text-emerald-500">{t('backupVerified')}</span> : <span className="text-amber-500">{t('backupNotVerified')}</span>}</td>
                      <td className="py-3"><div className="flex flex-wrap gap-2">
                        <button className="btn-secondary px-2.5 py-1 text-xs" disabled={busy} type="button" onClick={function download() { void downloadBackup(backup.id); }}>{t('backupDownload')}</button>
                        <button className="btn-secondary px-2.5 py-1 text-xs" disabled={busy} type="button" onClick={function verify() { verifyMutation.mutate(backup.id); }}>{verifyMutation.isPending ? t('loading') : t('backupVerify')}</button>
                        <button className="btn-secondary px-2.5 py-1 text-xs" disabled={busy} type="button" onClick={function restore() { if (window.confirm(t('backupRestoreConfirm'))) restoreMutation.mutate(backup.id); }}>{restoreMutation.isPending ? t('loading') : t('backupRestore')}</button>
                        <button className="btn-secondary px-2.5 py-1 text-xs text-rose-500" disabled={busy} type="button" onClick={function remove() { if (window.confirm(t('backupDeleteConfirm'))) deleteMutation.mutate(backup.id); }}>{t('delete')}</button>
                      </div></td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        ) : null}
      </section>

      <section className="card p-6 space-y-3">
        <h3 className="text-base font-semibold text-[var(--heading-color)]">{t('backupOperations')}</h3>
        {operationsQuery.data?.length ? operationsQuery.data.map(function renderOperation(operation, index) {
          return <div className="flex flex-wrap justify-between gap-2 border-t border-[var(--border-color)] pt-3 text-sm" key={`${operation.at}-${index}`}>
            <span>{new Date(operation.at).toLocaleString(i18n.language)} · {operation.action}</span>
            <span className={operation.status === 'success' ? 'text-emerald-500' : 'text-rose-500'}>{operation.message}</span>
          </div>;
        }) : <p className="text-sm text-[var(--text-muted)]">{t('backupNoOperations')}</p>}
      </section>
    </div>
  );
}

// Uptime Formatter Utility
function formatUptime(uptimeStr: string, lng: string): string {
  if (!uptimeStr || uptimeStr === 'unknown') {
    return lng.startsWith('ru') ? 'неизвестно' : 'unknown';
  }

  let hours = 0;
  let minutes = 0;
  let seconds = 0;

  const hoursMatch = uptimeStr.match(/(\d+)h/);
  const minutesMatch = uptimeStr.match(/(\d+)m/);
  const secondsMatch = uptimeStr.match(/(\d+)s/);

  if (hoursMatch) hours = parseInt(hoursMatch[1], 10);
  if (minutesMatch) minutes = parseInt(minutesMatch[1], 10);
  if (secondsMatch) seconds = parseInt(secondsMatch[1], 10);

  const days = Math.floor(hours / 24);
  const remHours = hours % 24;

  const pad = (num: number) => String(num).padStart(2, '0');
  const timeFormatted = `${pad(remHours)}:${pad(minutes)}:${pad(seconds)}`;

  if (lng.startsWith('ru')) {
    if (days > 0) {
      return `${days} дн. ${timeFormatted}`;
    }
    return timeFormatted;
  } else {
    if (days > 0) {
      return `${days} days ${timeFormatted}`;
    }
    return timeFormatted;
  }
}

function LanguageSwitch(): React.JSX.Element {
  const { i18n } = useTranslation();

  function handleLanguageChange(event: React.ChangeEvent<HTMLSelectElement>): void {
    const selectedLanguage = event.target.value;
    void i18n.changeLanguage(selectedLanguage);
    localStorage.setItem('megopanel-language', selectedLanguage);
  }

  return (
    <div className="relative inline-block">
      <select
        className="input max-w-[90px] bg-[var(--input-bg)] text-[var(--input-text)] border border-[var(--input-border)] rounded-md py-1.5 px-3 cursor-pointer text-sm uppercase"
        value={i18n.language.startsWith('ru') ? 'ru' : 'en'}
        onChange={handleLanguageChange}
      >
        <option value="en">EN</option>
        <option value="ru">RU</option>
      </select>
    </div>
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
    <button className="btn-secondary text-sm px-3 py-1.5" onClick={toggleTheme} type="button">
      {theme === 'dark' ? t('dark').toUpperCase() : t('light').toUpperCase()}
    </button>
  );
}

function Layout(properties: { children: React.ReactNode; showHeaderControls?: boolean }): React.JSX.Element {
  const { t } = useTranslation();
  const navigate = useNavigate();

  const handleLogout = () => {
    api.post('/auth/logout').then(() => {
      navigate('/login');
    }).catch(err => {
      console.error('Logout failed:', err);
      navigate('/login');
    });
  };

  return (
    <div className="min-h-screen bg-[var(--bg-gradient)] transition-colors duration-200">
      <header className="border-b border-[var(--border-color)] bg-[var(--card-bg)]/80 backdrop-blur-md sticky top-0 z-50">
        <div className="mx-auto max-w-5xl px-4 md:px-8 py-4 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <span className="text-sm font-semibold tracking-tight uppercase px-2 py-0.5 bg-zinc-900 text-white border border-zinc-800 rounded">
              MegoPanel
            </span>
            <span className="h-4 w-[1px] bg-[var(--border-color)] hidden sm:block"></span>
            <span className="text-sm text-[var(--text-muted)] font-mono hidden sm:block">v1.2.0</span>
          </div>

          <div className="flex items-center gap-4">
            <LanguageSwitch />
            <ThemeSwitch />
            {properties.showHeaderControls && (
              <button onClick={handleLogout} className="btn-secondary text-sm px-3 py-1.5 hover:text-rose-500 hover:border-rose-500/30">
                {t('logout')}
              </button>
            )}
          </div>
        </div>
      </header>

      <main className="mx-auto max-w-5xl px-4 md:px-8 py-8 md:py-12 animate-fadeIn">
        {properties.children}
      </main>
    </div>
  );
}

function Login(): React.JSX.Element {
  const { t } = useTranslation();
  const navigate = useNavigate();

  // ROUTE PROTECTION: Redirect to '/' if already authenticated or redirect to '/onboarding' if not configured
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
    enabled: setupQuery.data !== undefined && setupQuery.data.configured === true,
  });

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

  if (setupQuery.isLoading) {
    return <Layout><p className="text-sm text-[var(--text-muted)]">{t('loading')}</p></Layout>;
  }

  if (setupQuery.data?.configured === false) {
    return <Navigate to="/onboarding" />;
  }

  if (currentUserQuery.isSuccess) {
    return <Navigate to="/" />;
  }

  return (
    <Layout>
      <div className="flex flex-col items-center justify-center min-h-[60vh]">
        <form action={submitLogin} className="card w-full max-w-md p-8 space-y-6">
          <div className="text-center">
            <h2 className="text-2xl font-bold tracking-tight text-[var(--heading-color)]">{t('login')}</h2>
          </div>
          <div className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-[var(--text-muted)] mb-1 uppercase tracking-wider">{t('username')}</label>
              <input className="input" name="username" placeholder="admin" required />
            </div>
            <div>
              <label className="block text-sm font-medium text-[var(--text-muted)] mb-1 uppercase tracking-wider">{t('password')}</label>
              <input className="input" name="password" placeholder="••••••••" type="password" required />
            </div>
          </div>
          <button className="btn w-full py-2.5" type="submit" disabled={mutation.isPending}>
            {mutation.isPending ? '...' : t('login')}
          </button>
          {mutation.error ? (
            <p className="text-sm font-medium text-rose-500 text-center">
              Login failed. Please verify your credentials.
            </p>
          ) : null}
        </form>
      </div>
    </Layout>
  );
}

function Onboarding(): React.JSX.Element {
  const { t } = useTranslation();
  const [step, setStep] = React.useState(1);
  const [setupToken, setSetupToken] = React.useState('');
  const [dbRootPassword, setDbRootPassword] = React.useState('');

  // ROUTE PROTECTION: Redirect to '/' if already configured
  const setupQuery = useQuery({
    queryKey: ['setup'],
    queryFn: async function loadSetupStatus(): Promise<{ configured: boolean }> {
      const response = await api.get<{ configured: boolean }>('/setup/status');
      return response.data;
    },
  });

  const onboardingMariaDbStatusQuery = useQuery({
    queryKey: ['onboarding-mariadb-status'],
    queryFn: async function loadOnboardingMariaDbStatus(): Promise<{ name: string; installed: boolean }> {
      const response = await api.get<{ name: string; installed: boolean }>('/install/mariadb/status');
      return response.data;
    },
    enabled: step === 2,
  });

  const onboardingNginxStatusQuery = useQuery({
    queryKey: ['onboarding-nginx-status'],
    queryFn: async function loadOnboardingNginxStatus(): Promise<{ name: string; installed: boolean }> {
      const response = await api.get<{ name: string; installed: boolean }>('/install/nginx/status');
      return response.data;
    },
    enabled: step === 2,
  });

  const onboardingPhpmyadminStatusQuery = useQuery({
    queryKey: ['onboarding-phpmyadmin-status'],
    queryFn: async function loadOnboardingPhpmyadminStatus(): Promise<{ name: string; installed: boolean }> {
      const response = await api.get<{ name: string; installed: boolean }>('/install/phpmyadmin/status');
      return response.data;
    },
    enabled: step === 2,
  });

  const adminMutation = useMutation({
    mutationFn: function createAdministrator(formData: FormData) {
      const username = formData.get('username');
      const password = formData.get('password');
      return api.post('/setup/admin', { username: username, password: password, setupToken: setupToken });
    },
    onSuccess: function handleAdministratorCreated(): void {
      setStep(2);
    },
  });

  const installMariaDbMutation = useMutation({
    mutationFn: function installMariaDb() {
      return api.post('/install/mariadb', { remoteAccess: false, rootPassword: dbRootPassword });
    },
    onSuccess: function handleMariaDbInstalled(): void {
      void onboardingMariaDbStatusQuery.refetch();
    },
  });

  const installNginxMutation = useMutation({
    mutationFn: function installNginx() {
      return api.post('/install/nginx');
    },
    onSuccess: function handleNginxInstalled(): void {
      void onboardingNginxStatusQuery.refetch();
    },
  });

  const installPhpmyadminMutation = useMutation({
    mutationFn: function installPhpmyadmin() {
      return api.post('/install/phpmyadmin');
    },
    onSuccess: function handlePmaInstalled(): void {
      void onboardingPhpmyadminStatusQuery.refetch();
    },
  });

  function submitAdministrator(formData: FormData): void {
    const password = formData.get('password');
    const confirm = formData.get('confirm');

    // PASSWORD CONFIRMATION SECURITY CHECK (Vulnerability fix)
    if (password !== confirm) {
      alert(t('confirm') + ' error: passwords do not match!');
      return;
    }

    adminMutation.mutate(formData);
  }

  function installMariaDb(): void {
    installMariaDbMutation.mutate();
  }

  function installNginx(): void {
    installNginxMutation.mutate();
  }

  function installPhpmyadmin(): void {
    installPhpmyadminMutation.mutate();
  }

  if (setupQuery.isLoading) {
    return <Layout><p className="text-sm text-[var(--text-muted)]">{t('loading')}</p></Layout>;
  }

  if (setupQuery.data !== undefined && setupQuery.data.configured === true && step === 1) {
    return <Navigate to="/" />;
  }

  return (
    <Layout>
      <section className="card mx-auto max-w-2xl p-8 space-y-6">
        <div className="text-center border-b border-[var(--border-color)] pb-4">
          <h2 className="text-2xl font-bold tracking-tight text-[var(--heading-color)]">{t('onboarding')}</h2>
          <p className="text-sm text-[var(--text-muted)] mt-1">{t('setupDescription')}</p>
        </div>

        {step === 1 ? (
          <form action={submitAdministrator} className="space-y-4">
            <h3 className="text-sm font-semibold uppercase tracking-wider text-[var(--text-muted)]">{t('createAdmin')}</h3>
            <div>
              <label className="block text-sm font-medium text-[var(--text-muted)] mb-1 uppercase tracking-wider">{t('setupToken')}</label>
              <input
                className="input"
                name="setupToken"
                type="password"
                autoComplete="one-time-code"
                value={setupToken}
                onChange={function handleSetupTokenChange(e: React.ChangeEvent<HTMLInputElement>) { setSetupToken(e.target.value); }}
                required
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-[var(--text-muted)] mb-1 uppercase tracking-wider">{t('username')}</label>
              <input className="input" name="username" placeholder="admin" required />
            </div>
            <div>
              <label className="block text-sm font-medium text-[var(--text-muted)] mb-1 uppercase tracking-wider">{t('password')}</label>
              <input className="input" name="password" placeholder="••••••••" type="password" minLength={12} required />
            </div>
            <div>
              <label className="block text-sm font-medium text-[var(--text-muted)] mb-1 uppercase tracking-wider">{t('confirm')}</label>
              <input className="input" name="confirm" placeholder="••••••••" type="password" required />
            </div>
            <button className="btn w-full py-2.5 mt-2" type="submit" disabled={adminMutation.isPending}>
              {adminMutation.isPending ? '...' : t('next')}
            </button>
            {adminMutation.error ? <p className="text-sm font-semibold text-rose-500">{t('creationFailed')}</p> : null}
          </form>
        ) : null}

        {step === 2 ? (
          <div className="space-y-6">
            <div className="grid gap-6 sm:grid-cols-3">
              {/* MariaDB Card */}
              <div className="space-y-4 rounded-lg border border-[var(--border-color)] p-4 bg-[var(--bg-color)]/20 flex flex-col justify-between">
                <div className="space-y-3">
                  <div className="flex flex-col gap-2">
                    <h3 className="text-base font-semibold text-[var(--heading-color)]">MariaDB</h3>
                    <div>
                      <span className={`text-[10px] uppercase font-bold px-2 py-0.5 rounded ${
                        onboardingMariaDbStatusQuery.data !== undefined && onboardingMariaDbStatusQuery.data.installed === true
                          ? 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20'
                          : 'bg-zinc-500/10 text-zinc-400 border border-zinc-500/20'
                      }`}>
                        {onboardingMariaDbStatusQuery.data !== undefined && onboardingMariaDbStatusQuery.data.installed === true ? t('installed') : t('notInstalled')}
                      </span>
                    </div>
                  </div>

                  {onboardingMariaDbStatusQuery.data?.installed === true && (
                    <div className="space-y-1">
                      <label className="block text-[10px] font-semibold text-[var(--text-muted)] uppercase tracking-wider">{t('dbRootPasswordLabel')}</label>
                      <input
                        className="input py-1 px-2 text-xs"
                        type="password"
                        placeholder={t('dbRootPasswordPlaceholder')}
                        value={dbRootPassword}
                        onChange={function handleDbRootPasswordChange(e: React.ChangeEvent<HTMLInputElement>) { setDbRootPassword(e.target.value); }}
                      />
                    </div>
                  )}
                </div>
                <div>
                  {onboardingMariaDbStatusQuery.data?.installed === true ? (
                    <button
                      className="btn flex items-center justify-center gap-2 cursor-pointer w-full text-xs py-1.5"
                      onClick={installMariaDb}
                      type="button"
                      disabled={installMariaDbMutation.isPending}
                    >
                      {installMariaDbMutation.isPending ? (
                        <>
                          <svg className="animate-spin h-3.5 w-3.5 text-black dark:text-white" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                            <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                          </svg>
                          {t('installing')}
                        </>
                      ) : t('configureMariaDb')}
                    </button>
                  ) : onboardingMariaDbStatusQuery.data?.installed === false ? (
                    <p className="text-xs text-[var(--text-muted)]">{t('mariadbNeedsInstaller')}</p>
                  ) : null}
                  {installMariaDbMutation.error ? <p className="text-xs font-semibold text-rose-500">{apiErrorMessage(installMariaDbMutation.error, t('databaseConfigurationFailed'))}</p> : null}
                </div>
              </div>

              {/* Nginx Card */}
              <div className="space-y-4 rounded-lg border border-[var(--border-color)] p-4 bg-[var(--bg-color)]/20 flex flex-col justify-between">
                <div className="space-y-3">
                  <div className="flex flex-col gap-2">
                    <h3 className="text-base font-semibold text-[var(--heading-color)]">Nginx</h3>
                    <div>
                      <span className={`text-[10px] uppercase font-bold px-2 py-0.5 rounded ${
                        onboardingNginxStatusQuery.data !== undefined && onboardingNginxStatusQuery.data.installed === true
                          ? 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20'
                          : 'bg-zinc-500/10 text-zinc-400 border border-zinc-500/20'
                      }`}>
                        {onboardingNginxStatusQuery.data !== undefined && onboardingNginxStatusQuery.data.installed === true ? t('installed') : t('notInstalled')}
                      </span>
                    </div>
                  </div>

                  <p className="text-xs text-[var(--text-muted)]">{t('nginxDescription')}</p>
                </div>
                <div>
                  {(onboardingNginxStatusQuery.data === undefined || onboardingNginxStatusQuery.data.installed !== true) && (
                    <button
                      className="btn flex items-center justify-center gap-2 cursor-pointer w-full text-xs py-1.5"
                      onClick={installNginx}
                      type="button"
                      disabled={installNginxMutation.isPending}
                    >
                      {installNginxMutation.isPending ? (
                        <>
                          <svg className="animate-spin h-3.5 w-3.5 text-black dark:text-white" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                            <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                          </svg>
                          {t('installing')}
                        </>
                      ) : t('install')}
                    </button>
                  )}
                  {installNginxMutation.error ? <p className="text-xs font-semibold text-rose-500">Installation failed</p> : null}
                </div>
              </div>

              {/* phpMyAdmin Card */}
              <div className="space-y-4 rounded-lg border border-[var(--border-color)] p-4 bg-[var(--bg-color)]/20 flex flex-col justify-between">
                <div className="space-y-3">
                  <div className="flex flex-col gap-2">
                    <h3 className="text-base font-semibold text-[var(--heading-color)]">phpMyAdmin</h3>
                    <div>
                      <span className={`text-[10px] uppercase font-bold px-2 py-0.5 rounded ${
                        onboardingPhpmyadminStatusQuery.data !== undefined && onboardingPhpmyadminStatusQuery.data.installed === true
                          ? 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20'
                          : 'bg-zinc-500/10 text-zinc-400 border border-zinc-500/20'
                      }`}>
                        {onboardingPhpmyadminStatusQuery.data !== undefined && onboardingPhpmyadminStatusQuery.data.installed === true ? t('installed') : t('notInstalled')}
                      </span>
                    </div>
                  </div>

                  <p className="text-xs text-[var(--text-muted)]">{t('phpmyadminDescription')}</p>
                </div>
                <div>
                  {(onboardingPhpmyadminStatusQuery.data === undefined || onboardingPhpmyadminStatusQuery.data.installed !== true) && (
                    <button
                      className="btn flex items-center justify-center gap-2 cursor-pointer w-full text-xs py-1.5"
                      onClick={installPhpmyadmin}
                      type="button"
                      disabled={installPhpmyadminMutation.isPending}
                    >
                      {installPhpmyadminMutation.isPending ? (
                        <>
                          <svg className="animate-spin h-3.5 w-3.5 text-black dark:text-white" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                            <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                          </svg>
                          {t('installing')}
                        </>
                      ) : t('install')}
                    </button>
                  )}
                  {installPhpmyadminMutation.error ? <p className="text-xs font-semibold text-rose-500">Installation failed</p> : null}
                </div>
              </div>
            </div>

            <button className="btn w-full cursor-pointer py-2" onClick={function finishOnboarding(): void { window.location.href = '/'; }} type="button">
              {t('next')}
            </button>
          </div>
        ) : null}
      </section>
    </Layout>
  );
}

function Dashboard(): React.JSX.Element {
  const { t, i18n } = useTranslation();
  const [activeTab, setActiveTab] = React.useState<'websites' | 'databases' | 'backups' | 'settings'>('websites');

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
    mutationFn: function createDatabase(data: { name: string; charset: string; password?: string }) {
      return api.post('/databases', data);
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

  const [dashboardDbRootPassword, setDashboardDbRootPassword] = React.useState('');

  const installMariaDbMutation = useMutation({
    mutationFn: function installMariaDb() {
      return api.post('/install/mariadb', { remoteAccess: false, rootPassword: dashboardDbRootPassword });
    },
    onSuccess: function handleInstallSuccess() {
      void mariadbStatusQuery.refetch();
    },
  });

  const [dbNameInput, setDbNameInput] = React.useState('');
  const [dbCharsetInput, setDbCharsetInput] = React.useState('utf8');
  const [dbPasswordInput, setDbPasswordInput] = React.useState('');
  const [deleteConfirmDb, setDeleteConfirmDb] = React.useState<string | null>(null);

  function handleCreateDatabase(event: React.FormEvent) {
    event.preventDefault();
    if (!dbNameInput) {
      return;
    }
    createDatabaseMutation.mutate({ name: dbNameInput, charset: dbCharsetInput, password: dbPasswordInput }, {
      onSuccess: function resetDbForm(): void {
        setDbNameInput('');
        setDbPasswordInput('');
        setDbCharsetInput('utf8');
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
    <Layout showHeaderControls>
      {/* Node Meta Section & Uptime formatting */}
      {statsQuery.data && (
        <div className="flex flex-col md:flex-row md:items-center justify-between gap-6 border-b border-[var(--border-color)] pb-6 mb-8 animate-fadeIn">
          <div className="space-y-1">
            <span className="block text-xs uppercase tracking-wider text-[var(--text-muted)] font-semibold">{t('uptimeLabel')}</span>
            <span className="block text-2xl font-bold tracking-tight text-[var(--heading-color)]">
              {formatUptime(statsQuery.data.uptime, i18n.language)}
            </span>
          </div>
          <div className="flex flex-wrap items-center gap-x-6 gap-y-2 text-sm text-[var(--text-muted)] font-mono">
            <div><span className="text-[var(--text-color)] font-medium">OS:</span> {statsQuery.data.osVersion}</div>
          </div>
        </div>
      )}

      {/* Stats Grid */}
      <div className="grid gap-6 sm:grid-cols-3 mb-10 animate-fadeIn">
        {cards.map(function renderCard(card: CardData): React.JSX.Element {
          return (
            <article className="card p-6 flex flex-col justify-between" key={card.label}>
              <p className="text-xs font-semibold tracking-wider uppercase text-[var(--text-muted)]">{card.label}</p>
              <p className="mt-3 text-2xl font-bold tracking-tight text-[var(--heading-color)]">{card.value}</p>
            </article>
          );
        })}
      </div>

      {/* Navigation Tabs */}
      <div className="mb-8 border-b border-[var(--border-color)] flex flex-wrap gap-6 animate-fadeIn">
        <button
          className={`pb-3 px-1 font-semibold text-xs transition-all border-b-2 uppercase tracking-wider cursor-pointer ${
            activeTab === 'websites'
              ? 'border-[var(--accent-color)] text-[var(--text-color)] font-bold'
              : 'border-transparent text-[var(--text-muted)] hover:text-[var(--text-color)]'
          }`}
          onClick={function selectWebsitesTab() { setActiveTab('websites'); }}
          type="button"
        >
          {t('websites')}
        </button>
        <button
          className={`pb-3 px-1 font-semibold text-xs transition-all border-b-2 uppercase tracking-wider cursor-pointer ${
            activeTab === 'databases'
              ? 'border-[var(--accent-color)] text-[var(--text-color)] font-bold'
              : 'border-transparent text-[var(--text-muted)] hover:text-[var(--text-color)]'
          }`}
          onClick={function selectDatabasesTab() { setActiveTab('databases'); }}
          type="button"
        >
          {t('databases')}
        </button>
        <button
          className={`pb-3 px-1 font-semibold text-xs transition-all border-b-2 uppercase tracking-wider cursor-pointer ${
            activeTab === 'backups'
              ? 'border-[var(--accent-color)] text-[var(--text-color)] font-bold'
              : 'border-transparent text-[var(--text-muted)] hover:text-[var(--text-color)]'
          }`}
          onClick={function selectBackupsTab() { setActiveTab('backups'); }}
          type="button"
        >
          {t('backups')}
        </button>
        <button
          className={`pb-3 px-1 font-semibold text-xs transition-all border-b-2 uppercase tracking-wider cursor-pointer ${
            activeTab === 'settings'
              ? 'border-[var(--accent-color)] text-[var(--text-color)] font-bold'
              : 'border-transparent text-[var(--text-muted)] hover:text-[var(--text-color)]'
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
          <section className="card p-6 lg:col-span-1 h-fit space-y-4">
            <h3 className="text-base font-semibold text-[var(--heading-color)]">{t('createSite')}</h3>
            <p className="text-xs text-[var(--text-muted)]">{t('siteCreateDescription')}</p>
            <form onSubmit={handleCreateWebsite} className="space-y-4">
              <div>
                <label className="block text-xs font-medium text-[var(--text-muted)] mb-1 uppercase tracking-wider">{t('domain')}</label>
                <input
                  className="input"
                  value={domainInput}
                  onChange={function handleDomainInputChange(event: React.ChangeEvent<HTMLInputElement>): void { setDomainInput(event.target.value); }}
                  placeholder="example.com"
                  required
                />
              </div>
              <div>
                <label className="block text-xs font-medium text-[var(--text-muted)] mb-1 uppercase tracking-wider">{t('ipAddress')}</label>
                <input
                  className="input"
                  value={ipAddressInput}
                  onChange={function handleIpAddressInputChange(event: React.ChangeEvent<HTMLInputElement>): void { setIpAddressInput(event.target.value); }}
                  placeholder="192.0.2.10"
                  required
                />
              </div>
              {createWebsiteMutation.isError ? (
                <p className="text-sm font-semibold text-rose-500 mt-2">
                  {(createWebsiteMutation.error as any)?.response?.data?.error || t('siteExistsError')}
                </p>
              ) : null}
              {createWebsiteMutation.isSuccess ? (
                <p className="text-sm font-semibold text-emerald-400 mt-2">{t('siteAdded')}</p>
              ) : null}
              <button className="btn w-full mt-2 cursor-pointer" type="submit" disabled={createWebsiteMutation.isPending}>
                {createWebsiteMutation.isPending ? '...' : t('addSite')}
              </button>
            </form>
          </section>

          {/* Websites List */}
          <section className="card p-6 lg:col-span-2">
            <h3 className="text-base font-semibold mb-4 text-[var(--heading-color)]">{t('websites')}</h3>
            {websitesQuery.isLoading ? (
              <p className="text-sm text-[var(--text-muted)]">{t('loading')}</p>
            ) : !websitesQuery.data || websitesQuery.data.length === 0 ? (
              <div className="text-lg font-semibold text-[var(--text-muted)] text-center py-12">
                {t('noWebsites')}
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-left text-xs border-collapse">
                  <thead>
                    <tr className="border-b border-[var(--border-color)] text-[var(--table-header)] text-[10px] font-semibold uppercase tracking-wider">
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
                          <td className="py-3 px-4 text-[var(--text-muted)] font-mono">{site.ipAddress}</td>
                          <td className="py-3 px-4 text-[var(--text-muted)] font-mono">{site.path}</td>
                          <td className="py-3 px-4 text-right">
                            <button
                              className="btn-danger py-1 px-3 text-[10px] uppercase tracking-wider font-semibold cursor-pointer"
                              onClick={function deleteSelectedWebsite(): void {
                                if (window.confirm(t('websiteDeleteConfirm'))) {
                                  deleteWebsiteMutation.mutate(site.id);
                                }
                              }}
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
            <p className="text-sm text-[var(--text-muted)]">{t('loading')}</p>
          ) : !mariadbStatusQuery.data?.installed ? (
            <div className="card p-8 text-center max-w-xl mx-auto space-y-4">
              <h3 className="text-lg font-semibold text-[var(--heading-color)]">MariaDB</h3>
              <p className="text-[var(--text-muted)] text-sm">{t('mariadbNotInstalled')}</p>
            </div>
          ) : (
            <div className="space-y-8">
              <section className="card p-5 space-y-3">
                <div>
                  <h3 className="text-base font-semibold text-[var(--heading-color)]">{t('configureMariaDb')}</h3>
                  <p className="text-sm text-[var(--text-muted)]">{t('mariadbCredentialHelp')}</p>
                </div>
                <div className="flex flex-col sm:flex-row gap-3">
                  <input
                    className="input"
                    type="password"
                    placeholder={t('dbRootPasswordPlaceholder')}
                    value={dashboardDbRootPassword}
                    onChange={function handleDashboardDbRootPasswordChange(e: React.ChangeEvent<HTMLInputElement>) { setDashboardDbRootPassword(e.target.value); }}
                  />
                  <button
                    className="btn cursor-pointer shrink-0"
                    onClick={function configureDbService() { installMariaDbMutation.mutate(); }}
                    disabled={installMariaDbMutation.isPending}
                  >
                    {installMariaDbMutation.isPending ? t('installing') : t('configureMariaDb')}
                  </button>
                </div>
                {installMariaDbMutation.error ? <p className="text-sm text-rose-500">{apiErrorMessage(installMariaDbMutation.error, t('databaseConfigurationFailed'))}</p> : null}
                {installMariaDbMutation.isSuccess ? <p className="text-sm text-emerald-400">{t('databaseConfigured')}</p> : null}
              </section>
              <div className="grid gap-8 lg:grid-cols-3">
                {/* Create Database Form */}
                <section className="card p-6 lg:col-span-1 h-fit space-y-4">
                  <h3 className="text-base font-semibold text-[var(--heading-color)]">{t('createDb')}</h3>
                  <form onSubmit={handleCreateDatabase} className="space-y-4">
                    <div>
                      <label className="block text-xs font-medium text-[var(--text-muted)] mb-1 uppercase tracking-wider">{t('dbName')}</label>
                      <input
                        className="input"
                        value={dbNameInput}
                        onChange={function handleDbNameInputChange(event: React.ChangeEvent<HTMLInputElement>): void { setDbNameInput(event.target.value); }}
                        placeholder="my_database"
                        required
                      />
                    </div>
                    <div>
                      <label className="block text-xs font-medium text-[var(--text-muted)] mb-1 uppercase tracking-wider">{t('password')}</label>
                      <input
                        className="input"
                        type="password"
                        value={dbPasswordInput}
                        onChange={function handleDbPasswordInputChange(event: React.ChangeEvent<HTMLInputElement>): void { setDbPasswordInput(event.target.value); }}
                        placeholder={t('password')}
                      />
                    </div>
                    <div>
                      <label className="block text-xs font-medium text-[var(--text-muted)] mb-1 uppercase tracking-wider">{t('dbCharset')}</label>
                      <select
                        className="input bg-[var(--input-bg)] text-[var(--input-text)] border border-[var(--input-border)] rounded-md py-2 px-3 cursor-pointer"
                        value={dbCharsetInput}
                        onChange={function handleDbCharsetInputChange(event: React.ChangeEvent<HTMLSelectElement>): void { setDbCharsetInput(event.target.value); }}
                      >
                        <option value="utf8">utf8</option>
                        <option value="utf8mb4">utf8mb4</option>
                        <option value="cp1251">cp1251</option>
                        <option value="latin1">latin1</option>
                      </select>
                    </div>
                    {createDatabaseMutation.isError ? (
                      <p className="text-sm font-semibold text-rose-500 mt-2">
                        {(createDatabaseMutation.error as any)?.response?.data?.error || t('dbExistsError')}
                      </p>
                    ) : null}
                    {createDatabaseMutation.isSuccess ? (
                      <p className="text-sm font-semibold text-emerald-400 mt-2">{t('dbCreated')}</p>
                    ) : null}
                    <button className="btn w-full mt-2 cursor-pointer" type="submit" disabled={createDatabaseMutation.isPending}>
                      {createDatabaseMutation.isPending ? '...' : t('addDb')}
                    </button>
                  </form>
                </section>

                {/* Databases List */}
                <section className="card p-6 lg:col-span-2">
                  <h3 className="text-base font-semibold mb-4 text-[var(--heading-color)]">{t('databases')}</h3>
                  {databasesQuery.isLoading ? (
                    <p className="text-sm text-[var(--text-muted)]">{t('loadingDatabases')}</p>
                  ) : !databasesQuery.data || databasesQuery.data.length === 0 ? (
                    <div className="text-lg font-semibold text-[var(--text-muted)] text-center py-12">
                      {t('noDatabases')}
                    </div>
                  ) : (
                    <div className="overflow-x-auto">
                      <table className="w-full text-left text-xs border-collapse">
                        <thead>
                          <tr className="border-b border-[var(--border-color)] text-[var(--table-header)] text-[10px] font-semibold uppercase tracking-wider">
                            <th className="py-3 px-4">{t('dbName')}</th>
                            <th className="py-3 px-4 text-right"></th>
                          </tr>
                        </thead>
                        <tbody>
                          {databasesQuery.data.map(function renderDatabase(db: string): React.JSX.Element {
                            const isConfirming = deleteConfirmDb === db;
                            return (
                              <tr className="border-b border-[var(--border-color)] hover:bg-[var(--table-hover)] transition duration-150" key={db}>
                                <td className="py-3 px-4 font-semibold text-[var(--text-color)]">{db}</td>
                                <td className="py-3 px-4 text-right">
                                  <div className="flex items-center justify-end gap-2">
                                    {isConfirming ? (
                                      <>
                                        <button
                                          className="btn-danger cursor-pointer py-1 px-3 text-[10px] uppercase font-bold tracking-wider"
                                          onClick={function confirmDelete(): void {
                                            const confirmMessage = i18n.language.startsWith('ru')
                                              ? `Вы уверены, что хотите удалить базу данных "${db}"?`
                                              : `Are you sure you want to delete database "${db}"?`;
                                            if (window.confirm(confirmMessage)) {
                                              deleteDatabaseMutation.mutate(db, {
                                                onSuccess: function() {
                                                  setDeleteConfirmDb(null);
                                                }
                                              });
                                            }
                                          }}
                                          disabled={deleteDatabaseMutation.isPending}
                                        >
                                          {t('confirmDelete')}
                                        </button>
                                        <button
                                          className="btn-secondary cursor-pointer py-1 px-3 text-[10px] uppercase font-bold tracking-wider"
                                          onClick={function cancelDelete(): void { setDeleteConfirmDb(null); }}
                                        >
                                          {t('cancel')}
                                        </button>
                                      </>
                                    ) : (
                                      <>
                                        {phpmyadminStatusQuery.data?.installed && (
                                          <button
                                            className="btn-secondary cursor-pointer py-1 px-3 text-[10px] uppercase font-bold tracking-wider"
                                            onClick={function openAutologin(): void {
                                              api.post<{ url: string }>('/phpmyadmin/autologin', { db }).then(function(res) {
                                                const cleanPath = res.data.url.replace(/^\/phpmyadmin/, '');
                                                const autologinUrl = window.location.protocol + '//' + window.location.hostname + ':8080' + cleanPath;
                                                window.location.href = autologinUrl;
                                              }).catch(function(err) {
                                                console.error('Autologin failed:', err);
                                              });
                                            }}
                                          >
                                            {t('open')}
                                          </button>
                                        )}
                                        <button
                                          className="btn-danger cursor-pointer py-1 px-3 text-[10px] uppercase font-bold tracking-wider"
                                          onClick={function startDelete(): void { setDeleteConfirmDb(db); }}
                                        >
                                          {t('delete')}
                                        </button>
                                      </>
                                    )}
                                  </div>
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
              <section className="card p-6">
                <div className="flex flex-col md:flex-row items-start md:items-center justify-between gap-4">
                  <div>
                    <h3 className="text-base font-semibold text-[var(--heading-color)] mb-1">{t('phpMyAdminStatus')}</h3>
                    <p className="text-[var(--text-muted)] text-sm">
                      {phpmyadminStatusQuery.data?.installed ? t('phpMyAdminInstalled') : t('phpMyAdminNotInstalled')}
                    </p>
                  </div>

                  {phpmyadminStatusQuery.isLoading ? (
                    <p className="text-sm text-[var(--text-muted)]">{t('loading')}</p>
                  ) : phpmyadminStatusQuery.data?.installed ? (
                    <button
                      className="btn cursor-pointer py-2 px-4"
                      onClick={function openPma() {
                        api.post<{ url: string }>('/phpmyadmin/autologin', { db: '' }).then(function(res) {
                          const cleanPath = res.data.url.replace(/^\/phpmyadmin/, '');
                          const autologinUrl = window.location.protocol + '//' + window.location.hostname + ':8080' + cleanPath;
                          window.location.href = autologinUrl;
                        }).catch(function(err) {
                          console.error('Autologin failed:', err);
                          window.location.href = window.location.protocol + '//' + window.location.hostname + ':8080';
                        });
                      }}
                    >
                      {t('openPhpMyAdmin')}
                    </button>
                  ) : (
                    <button
                      className="btn flex items-center justify-center gap-2 cursor-pointer py-2 px-4"
                      onClick={function installPma() { installPhpmyadminMutation.mutate(); }}
                      disabled={installPhpmyadminMutation.isPending}
                    >
                      {installPhpmyadminMutation.isPending ? (
                        <>
                          <svg className="animate-spin h-3.5 w-3.5 text-black dark:text-white" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                            <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                          </svg>
                          {t('pmaInstalling')}
                        </>
                      ) : t('installPhpMyAdmin')}
                    </button>
                  )}
                </div>
              </section>
            </div>
          )}
        </div>
      ) : null}

      {activeTab === 'settings' ? (
        <div className="grid gap-8 lg:grid-cols-2 animate-fadeIn">
          <section className="card p-6 space-y-6">
            <h3 className="text-base font-semibold text-[var(--heading-color)] border-b border-[var(--border-color)] pb-3">{t('settings')}</h3>

            {/* Language Setting */}
            <div className="flex items-center justify-between py-2">
              <div>
                <p className="font-semibold text-sm text-[var(--text-color)]">{t('language')}</p>
                <p className="text-sm text-[var(--text-muted)]">{t('changeLanguage')}</p>
              </div>
              <LanguageSwitch />
            </div>

            {/* Theme Setting */}
            <div className="flex items-center justify-between py-2">
              <div>
                <p className="font-semibold text-sm text-[var(--text-color)]">{t('theme')}</p>
                <p className="text-sm text-[var(--text-muted)]">{t('switchTheme')}</p>
              </div>
              <ThemeSwitch />
            </div>
          </section>

          {/* Update Section */}
          <section className="card p-6 space-y-6 flex flex-col justify-between">
            <div className="space-y-4">
              <h3 className="text-base font-semibold text-[var(--heading-color)] border-b border-[var(--border-color)] pb-3">{t('updateCheck')}</h3>
              <div className="space-y-2">
                <p className="text-sm text-[var(--text-color)]">
                  <span className="font-semibold text-[var(--text-muted)] mr-2">{t('updateCurrent')}</span>
                  <span className="font-mono text-zinc-400">v1.2.0</span>
                </p>

                {/* State-driven display for latest update status */}
                <UpdateComponent />
              </div>
            </div>
          </section>
        </div>
      ) : null}

      {activeTab === 'backups' ? <BackupsPanel /> : null}
    </Layout>
  );
}

function Gate(): React.JSX.Element {
  const { t } = useTranslation();
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
    enabled: setupQuery.data !== undefined && setupQuery.data.configured === true,
  });

  if (setupQuery.isLoading) {
    return <Layout><p className="text-sm text-[var(--text-muted)]">{t('loading')}</p></Layout>;
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
