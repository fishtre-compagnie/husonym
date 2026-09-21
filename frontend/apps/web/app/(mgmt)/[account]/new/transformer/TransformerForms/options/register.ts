import type { DescMessage } from '@bufbuild/protobuf';
import type { OptionsFormSpec } from './OptionsForm';

// A transformer's options: the message of its config, and how to present each field.
export interface RegisteredOptions {
  schema: DescMessage;
  options: OptionsFormSpec<DescMessage>[];
}

// Declares the options of a transformer. The field names are checked against its config message
// here; the registry then keeps them under one type, since it holds every transformer's.
export function registerOptions<S extends DescMessage>(
  schema: S,
  options: OptionsFormSpec<S>[]
): RegisteredOptions {
  return { schema, options } as unknown as RegisteredOptions;
}
