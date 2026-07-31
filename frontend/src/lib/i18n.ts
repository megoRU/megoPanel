import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
const resources = {
  en: {
    translation: {
      login: 'Login',
      username: 'Username',
      password: 'Password',
      confirm: 'Confirm password',
      dashboard: 'Dashboard',
      onboarding: 'Onboarding',
      createAdmin: 'Create administrator',
      next: 'Next',
      install: 'Install',
      installed: 'Installed',
      notInstalled: 'Not Installed',
      remote: 'Enable remote access',
      logout: 'Logout',
      cpu: 'CPU usage',
      ram: 'RAM usage',
      disk: 'Disk usage',
      uptime: 'Uptime',
      os: 'OS version',
      hostname: 'Hostname',
      websites: 'Websites',
      domain: 'Domain',
      path: 'Path',
      createSite: 'Create Website',
      delete: 'Delete',
      siteAdded: 'Website added successfully',
      create: 'Create',
      addSite: 'Add Website'
    }
  },
  ru: {
    translation: {
      login: 'Войти',
      username: 'Имя пользователя',
      password: 'Пароль',
      confirm: 'Подтвердите пароль',
      dashboard: 'Панель',
      onboarding: 'Настройка',
      createAdmin: 'Создать администратора',
      next: 'Далее',
      install: 'Установить',
      installed: 'Установлено',
      notInstalled: 'Не установлено',
      remote: 'Включить удалённый доступ',
      logout: 'Выйти',
      cpu: 'ЦП',
      ram: 'Память',
      disk: 'Диск',
      uptime: 'Аптайм',
      os: 'Версия ОС',
      hostname: 'Хост',
      websites: 'Сайты',
      domain: 'Домен',
      path: 'Путь',
      createSite: 'Создать сайт',
      delete: 'Удалить',
      siteAdded: 'Сайт успешно добавлен',
      create: 'Создать',
      addSite: 'Добавить сайт'
    }
  }
};
const savedLanguage = typeof window !== 'undefined' ? (localStorage.getItem('megopanel-language') || 'en') : 'en';
void i18n.use(initReactI18next).init({ resources: resources, lng: savedLanguage, fallbackLng: 'en', interpolation: { escapeValue: false } });
export default i18n;
