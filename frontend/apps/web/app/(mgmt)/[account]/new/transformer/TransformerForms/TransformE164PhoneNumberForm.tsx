'use client';
import {
  TransformE164PhoneNumber,
  TransformE164PhoneNumberSchema,
} from '@husonym/sdk';
import { ReactElement } from 'react';
import OptionsForm from './options/OptionsForm';
import { TransformerConfigProps } from './util';

interface Props extends TransformerConfigProps<TransformE164PhoneNumber> {}

export default function TransformE164NumberForm(props: Props): ReactElement {
  return (
    <OptionsForm
      {...props}
      schema={TransformE164PhoneNumberSchema}
      options={[
        {
          field: 'preserveLength',
          label: 'Preserve Length',
          description:
            'Set the length of the output e164 phone number to be the same as the input e164 phone number.',
        },
      ]}
    />
  );
}
