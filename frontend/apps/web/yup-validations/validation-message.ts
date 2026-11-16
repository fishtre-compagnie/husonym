import english from '../messages/en/validation.json';

// The schemas keep their English messages: they are both what English shows and the
// source text a translation is found by. Each message of `validation` is the English
// text of a schema message, its `{params}` standing for the parts that vary (a bound, a
// field path, an error), so a message yup gave is matched to its key, its params read
// back from it. The match ignores case and a final period. A message that matches
// nothing (from the server, a library, a schema not listed yet) is shown as it is.

interface Entry {
  key: string;
  params: string[];
  pattern: RegExp;
}

function normalize(text: string): string {
  return text.trim().replace(/\.$/, '');
}

function toEntries(messages: object, prefix = ''): Entry[] {
  return Object.entries(messages).flatMap(([name, value]): Entry[] => {
    const key = prefix ? `${prefix}.${name}` : name;
    if (typeof value !== 'string') {
      return toEntries(value as object, key);
    }
    const params: string[] = [];
    const source = normalize(value)
      .split(/(\{\w+\})/)
      .map((part) => {
        const param = /^\{(\w+)\}$/.exec(part);
        if (param) {
          params.push(param[1]);
          return '(.+?)';
        }
        return part.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
      })
      .join('');
    return [{ key, params, pattern: new RegExp(`^${source}$`, 'is') }];
  });
}

// The fixed messages first, then the ones with the fewest parts that vary: the most
// precise match wins ("Name is a required field" before "{path} is a required field").
const ENTRIES = toEntries(english.validation).sort(
  (a, b) => a.params.length - b.params.length
);

/** The key of `validation` a message stands for, and the values of its params. */
export function matchValidationMessage(
  message: string
): { key: string; values: Record<string, string> } | undefined {
  const text = normalize(message);
  for (const entry of ENTRIES) {
    const match = entry.pattern.exec(text);
    if (match) {
      const values = Object.fromEntries(
        entry.params.map((param, idx) => [param, match[idx + 1]])
      );
      return { key: entry.key, values };
    }
  }
  return undefined;
}
