'use client';
import { TransformFirstName, TransformFirstNameSchema } from '@husonym/sdk';
import { ReactElement } from 'react';
import OptionsForm from './options/OptionsForm';
import { TransformerConfigProps } from './util';

interface Props extends TransformerConfigProps<TransformFirstName> {}

export default function TransformFirstNameForm(props: Props): ReactElement {
  return (
    <OptionsForm
      {...props}
      schema={TransformFirstNameSchema}
      options={[
        {
          field: 'preserveLength',
          label: 'Preserve Length',
          description:
            'Set the length of the output first name to be the same as the input',
        },
      ]}
    />
  );
}
