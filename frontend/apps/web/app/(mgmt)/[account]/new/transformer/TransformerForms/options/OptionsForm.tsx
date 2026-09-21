'use client';
import { DescMessage, MessageShape } from '@bufbuild/protobuf';
import { ReactElement } from 'react';
import { TransformerConfigProps } from '../util';
import OptionField, { OptionSpec } from './OptionField';

export interface OptionsFormSpec<S extends DescMessage> extends OptionSpec<S> {
  // Disables the option while another one makes it moot.
  disabledWhen?(value: MessageShape<S>): boolean;
}

interface Props<S extends DescMessage> extends TransformerConfigProps<
  MessageShape<S>
> {
  schema: S;
  options: OptionsFormSpec<S>[];
}

// The options form of a transformer, declared as a list: each entry names a field of
// the config message and how to present it.
export default function OptionsForm<S extends DescMessage>(
  props: Props<S>
): ReactElement {
  const { schema, options, value, setValue, isDisabled, errors } = props;

  return (
    <div className="flex flex-col w-full space-y-4">
      {options.map(({ disabledWhen, ...option }) => (
        <OptionField
          key={option.field}
          {...option}
          schema={schema}
          value={value}
          setValue={setValue}
          isDisabled={isDisabled || disabledWhen?.(value) === true}
          errors={errors}
        />
      ))}
    </div>
  );
}
