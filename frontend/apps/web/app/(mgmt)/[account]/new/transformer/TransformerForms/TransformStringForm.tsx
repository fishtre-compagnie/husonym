'use client';
import { TransformString, TransformStringSchema } from '@husonym/sdk';
import { ReactElement } from 'react';
import OptionsForm from './options/OptionsForm';
import { TransformerConfigProps } from './util';

interface Props extends TransformerConfigProps<TransformString> {}

export default function TransformStringForm(props: Props): ReactElement {
  return (
    <OptionsForm
      {...props}
      schema={TransformStringSchema}
      options={[
        {
          field: 'preserveLength',
          label: 'Preserve Length',
          description:
            'Set the length of the output string to be the same as the input',
        },
      ]}
    />
  );
}
