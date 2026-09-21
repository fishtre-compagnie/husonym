'use client';
import { GenerateFloat64, GenerateFloat64Schema } from '@husonym/sdk';
import { ReactElement } from 'react';
import OptionsForm from './options/OptionsForm';
import { TransformerConfigProps } from './util';

interface Props extends TransformerConfigProps<GenerateFloat64> {}

export default function GenerateFloat64Form(props: Props): ReactElement {
  return (
    <OptionsForm
      {...props}
      schema={GenerateFloat64Schema}
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
          description: 'Sets a minimum range for generated float64 value.',
        },
        {
          field: 'max',
          label: 'Maximum Value',
          description: 'Sets a maximum range for generated float64 value.',
        },
        {
          field: 'precision',
          label: 'Precision',
          description:
            'Sets the precision for the entire float64 value, not just the decimals. For example. a precision of 4 would update a float64 value of 23.567 to 23.56.',
        },
      ]}
    />
  );
}
