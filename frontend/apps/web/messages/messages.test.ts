import { readFileSync } from 'fs';
import { join } from 'path';
import { MESSAGE_FILES } from '.';

// Every message has its translation, and nothing else: the languages hold the same keys.
function keys(value: unknown, prefix = ''): string[] {
  if (value === null || typeof value !== 'object') {
    return [prefix];
  }
  return Object.entries(value).flatMap(([k, v]) =>
    keys(v, prefix ? `${prefix}.${k}` : k)
  );
}

function read(locale: string, file: string): unknown {
  return JSON.parse(
    readFileSync(join(__dirname, locale, `${file}.json`), 'utf8')
  );
}

describe('messages', () => {
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
