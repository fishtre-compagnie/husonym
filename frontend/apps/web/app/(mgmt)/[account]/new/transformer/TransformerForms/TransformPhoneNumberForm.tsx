'use client';
import { TransformPhoneNumber, TransformPhoneNumberSchema } from '@husonym/sdk';
import { ReactElement } from 'react';
import OptionsForm from './options/OptionsForm';
import { TransformerConfigProps } from './util';

interface Props extends TransformerConfigProps<TransformPhoneNumber> {}

export default function TransformPhoneNumberForm(props: Props): ReactElement {
  return (
    <OptionsForm
      {...props}
      schema={TransformPhoneNumberSchema}
      options={[
        {
          field: 'preserveLength',
          label: 'Preserve Length',
          description:
            'Set the length of the output phone number to be the same as the input',
        },
      ]}
    />
  );
}
