import type messages from '../messages/en.json';
import type { Locale } from './config';

// Types the message keys (from the English messages, the reference) and the locales.
declare module 'next-intl' {
  interface AppConfig {
    Locale: Locale;
    Messages: typeof messages;
  }
}
