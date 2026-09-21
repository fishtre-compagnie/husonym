'use client';
import { TransformFloat64, TransformFloat64Schema } from '@husonym/sdk';
import { ReactElement } from 'react';
import OptionsForm from './options/OptionsForm';
import { TransformerConfigProps } from './util';

interface Props extends TransformerConfigProps<TransformFloat64> {}

export default function TransformFloat64Form(props: Props): ReactElement {
  return (
    <OptionsForm
      {...props}
      schema={TransformFloat64Schema}
      options={[
        {
          field: 'randomizationRangeMin',
          label: 'Relative Minimum Range Value',
          description:
            'Sets a relative minimum lower range value. This will create a lowerbound around the source input value. For example, if the input value is 10, and you set this value to 5, then the minimum range will be 5 (10 - 5 = 5).',
        },
        {
          field: 'randomizationRangeMax',
          label: 'Relative Maximum Range Value',
          description:
            'Sets a relative maximum upper range value. This will create an upperbound around the source input value. For example, if the input value is 10, and you set this value to 5, then the maximum range will be 15 (10 + 5 = 15).',
        },
      ]}
    />
  );
}
