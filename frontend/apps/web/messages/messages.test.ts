import { readdirSync, readFileSync } from 'fs';
import { join } from 'path';

// Every message has its translation, and nothing else: the languages hold the same keys.
function keys(value: unknown, prefix = ''): string[] {
  if (value === null || typeof value !== 'object') {
    return [prefix];
  }
  return Object.entries(value).flatMap(([k, v]) =>
    keys(v, prefix ? `${prefix}.${k}` : k)
  );
}

// The areas, as the English files list them: the reference language.
const MESSAGE_FILES = readdirSync(join(__dirname, 'en'))
  .filter((f) => f.endsWith('.json'))
  .map((f) => f.replace(/\.json$/, ''));

function read(locale: string, file: string): unknown {
  return JSON.parse(
    readFileSync(join(__dirname, locale, `${file}.json`), 'utf8')
  );
}

describe('messages', () => {
  it('every language has the files of the English one', () => {
    for (const locale of readdirSync(__dirname).filter(
      (d) => d !== 'en' && !d.includes('.')
    )) {
      expect(readdirSync(join(__dirname, locale)).sort()).toEqual(
        readdirSync(join(__dirname, 'en')).sort()
      );
    }
  });

  it.each(MESSAGE_FILES)('%s has the same keys in every language', (file) => {
    const en = keys(read('en', file)).sort();
    expect(keys(read('fr', file)).sort()).toEqual(en);
  });

  it.each(MESSAGE_FILES)(
    '%s holds only its own namespace, shell aside',
    (file) => {
      if (file === 'shell') {
        return;
      }
      expect(Object.keys(read('en', file) as object)).toEqual([file]);
    }
  );
});
