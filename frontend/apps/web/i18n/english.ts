// The English words of a part of the messages, for helpers called where next-intl is
// not loaded (a test under jest) or by a caller that has no language to give: they
// read as they always did. Made without next-intl, from the English message files.

type Values = Record<string, string | number>;

// The English message of a key, such as "range.withZone", under a part of the messages.
function englishMessage(root: unknown, key: string): string {
  let node: unknown = root;
  for (const part of key.split('.')) {
    node = (node as Record<string, unknown> | undefined)?.[part];
  }
  return typeof node === 'string' ? node : key;
}

// Fills a message with its values. The messages read this way hold nothing but plain
// arguments and plurals of one and other, which is all this reads.
function fill(message: string, values: Values = {}): string {
  const plurals = message.replace(
    /\{(\w+), plural, one \{([^{}]*)\} other \{([^{}]*)\}\}/g,
    (_, name: string, one: string, other: string) => {
      const count = values[name];
      return (count === 1 ? one : other).replace(/#/g, String(count));
    }
  );
  return plurals.replace(/\{(\w+)\}/g, (whole, name: string) =>
    name in values ? String(values[name]) : whole
  );
}

/**
 * A translator in English of the messages under `root` (a part of an English message
 * file), to cast to the translator type of that namespace.
 */
export function englishTranslator(
  root: unknown
): (key: string, values?: Values) => string {
  return (key, values) => fill(englishMessage(root, key), values);
}
