import type { MessageShape } from '@bufbuild/protobuf';
import type { TransformerConfig } from '@husonym/sdk';
import {
  formatNumber,
  getEnumChoices,
  getOptionField,
  getOptionKind,
  readOptionValue,
} from './fields';
import { TRANSFORMER_OPTIONS } from './registry';

const MAX_TEXT = 30;

function shorten(text: string): string {
  return text.length > MAX_TEXT ? `${text.slice(0, MAX_TEXT - 1)}…` : text;
}

// The options of a config that say something, in a few words each: a switch that is on, a number
// or a text that is set, the value chosen in a list. A switch that is off, a field left empty, and
// an option another one makes moot are left out — the summary is read, not audited.
export function summarizeOptions(config?: TransformerConfig): string[] {
  const configCase = config?.config.case;
  if (!configCase) {
    return [];
  }
  const registered = TRANSFORMER_OPTIONS[configCase];
  if (!registered) {
    return [];
  }
  const value = config.config.value as MessageShape<typeof registered.schema>;

  const out: string[] = [];
  for (const option of registered.options) {
    if (option.disabledWhen?.(value)) {
      continue;
    }
    const label =
      typeof option.label === 'string' ? option.label : option.field;
    const field = getOptionField(registered.schema, option.field);
    const current = readOptionValue(value, option.field);
    switch (getOptionKind(field)) {
      case 'bool':
        if (current === true) {
          out.push(label);
        }
        break;
      case 'integer':
      case 'float':
        if (formatNumber(current) !== '') {
          out.push(`${label}: ${formatNumber(current)}`);
        }
        break;
      case 'string':
        if (typeof current === 'string' && current !== '') {
          out.push(`${label}: ${shorten(current)}`);
        }
        break;
      case 'enum': {
        const choice = getEnumChoices(field, option.formatEnumValue).find(
          (c) => c.number === current
        );
        if (choice) {
          out.push(`${label}: ${choice.label}`);
        }
        break;
      }
      case 'stringList':
        if (Array.isArray(current) && current.length > 0) {
          out.push(`${label}: ${shorten(current.join(', '))}`);
        }
        break;
    }
  }
  return out;
}
