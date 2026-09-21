'use client';
import {
  TransformInt64PhoneNumber,
  TransformInt64PhoneNumberSchema,
} from '@husonym/sdk';
import { ReactElement } from 'react';
import OptionsForm from './options/OptionsForm';
import { TransformerConfigProps } from './util';

interface Props extends TransformerConfigProps<TransformInt64PhoneNumber> {}

export default function TransformIntPhoneNumberForm(
  props: Props
): ReactElement {
  return (
    <OptionsForm
      {...props}
      schema={TransformInt64PhoneNumberSchema}
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
