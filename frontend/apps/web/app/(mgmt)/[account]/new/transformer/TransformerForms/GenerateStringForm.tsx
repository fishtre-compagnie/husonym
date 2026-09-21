'use client';
import { GenerateString, GenerateStringSchema } from '@husonym/sdk';
import { ReactElement } from 'react';
import OptionsForm from './options/OptionsForm';
import { TransformerConfigProps } from './util';

interface Props extends TransformerConfigProps<GenerateString> {}

export default function GenerateStringForm(props: Props): ReactElement {
  return (
    <OptionsForm
      {...props}
      schema={GenerateStringSchema}
      options={[
        {
          field: 'min',
          label: 'Minimum Length',
          description: 'Set the minimum length range of the output string.',
        },
        {
          field: 'max',
          label: 'Maximum Length',
          description: 'Set the maximum length range of the output string.',
        },
      ]}
    />
  );
}
