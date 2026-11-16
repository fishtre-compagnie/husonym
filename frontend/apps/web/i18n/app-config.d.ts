import type connections from '../messages/en/connections.json';
import type dashboard from '../messages/en/dashboard.json';
import type jobs from '../messages/en/jobs.json';
import type mapping from '../messages/en/mapping.json';
import type newJob from '../messages/en/newJob.json';
import type runs from '../messages/en/runs.json';
import type settings from '../messages/en/settings.json';
import type shell from '../messages/en/shell.json';
import type { Locale } from './config';

// Types the message keys (from the English messages, the reference) and the locales.
declare module 'next-intl' {
  interface AppConfig {
    Locale: Locale;
    Messages: typeof shell &
      typeof jobs &
      typeof mapping &
      typeof newJob &
      typeof runs &
      typeof dashboard &
      typeof connections &
      typeof settings;
  }
}
