'use client';
import { GenerateInt64, GenerateInt64Schema } from '@husonym/sdk';
import { ReactElement } from 'react';
import OptionsForm from './options/OptionsForm';
import { TransformerConfigProps } from './util';

interface Props extends TransformerConfigProps<GenerateInt64> {}

export default function GenerateInt64Form(props: Props): ReactElement {
  return (
    <OptionsForm
      {...props}
      schema={GenerateInt64Schema}
      options={[
        {
          field: 'randomizeSign',
          label: 'Randomize Sign',
          description:
            'Will randomly assign the sign. This may cause the generated value to be out of the defined min/max range. If the min/max is 20-40, the value may be in the following ranges: 20 <= x <= 40 and -40 <= x <= -20',
        },
        {
          field: 'min',
          label: 'Minimum Value',
          description: 'Sets a minimum range for generated int64 value.',
        },
        {
          field: 'max',
          label: 'Maximum Value',
          description: 'Sets a maximum range for generated int64 value.',
        },
      ]}
    />
  );
}
