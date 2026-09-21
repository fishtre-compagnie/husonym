'use client';
import { TransformLastName, TransformLastNameSchema } from '@husonym/sdk';
import { ReactElement } from 'react';
import OptionsForm from './options/OptionsForm';
import { TransformerConfigProps } from './util';

interface Props extends TransformerConfigProps<TransformLastName> {}

export default function TransformLastNameForm(props: Props): ReactElement {
  return (
    <OptionsForm
      {...props}
      schema={TransformLastNameSchema}
      options={[
        {
          field: 'preserveLength',
          label: 'Preserve Length',
          description:
            'Set the length of the output last name to be the same as the input',
        },
      ]}
    />
  );
}
