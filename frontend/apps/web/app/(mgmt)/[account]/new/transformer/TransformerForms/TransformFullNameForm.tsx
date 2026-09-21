'use client';
import { TransformFullName, TransformFullNameSchema } from '@husonym/sdk';
import { ReactElement } from 'react';
import OptionsForm from './options/OptionsForm';
import { TransformerConfigProps } from './util';

interface Props extends TransformerConfigProps<TransformFullName> {}

export default function TransformFullNameForm(props: Props): ReactElement {
  return (
    <OptionsForm
      {...props}
      schema={TransformFullNameSchema}
      options={[
        {
          field: 'preserveLength',
          label: 'Preserve Length',
          description:
            'Generates a full name which has the same first name and last name length as the input first and last names',
        },
      ]}
    />
  );
}
