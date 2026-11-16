import type { Locale } from '@/i18n/config';

// The messages of the UI, one file per area of the app and per language, so that
// areas are translated side by side without touching the same file. Each file
// holds the namespaces of its area: `shell.json` the app's frame (navigation,
// header, menus) and the shared `common` words; the others one namespace named
// after the file (`jobs.json` → `jobs`). Adding an area means adding its files
// in every language, and its name here and in i18n/app-config.d.ts.
const MESSAGE_FILES = [
  'shell',
  'jobs',
  'mapping',
  'newJob',
  'runs',
  'dashboard',
  'connections',
  'settings',
  'validation',
] as const;

/** Every message of a language, the files merged into one object. */
export async function loadMessages(
  locale: Locale
): Promise<Record<string, unknown>> {
  const files = await Promise.all(
    MESSAGE_FILES.map(
      async (file) =>
        (await import(`./${locale}/${file}.json`)).default as Record<
          string,
          unknown
        >
    )
  );
  return Object.assign({}, ...files);
}
