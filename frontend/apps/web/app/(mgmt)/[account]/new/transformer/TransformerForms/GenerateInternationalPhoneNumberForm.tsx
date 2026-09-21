'use client';
import {
  GenerateE164PhoneNumber,
  GenerateE164PhoneNumberSchema,
} from '@husonym/sdk';
import { ReactElement } from 'react';
import OptionsForm from './options/OptionsForm';
import { TransformerConfigProps } from './util';

interface Props extends TransformerConfigProps<GenerateE164PhoneNumber> {}

export default function GenerateInternationalPhoneNumberForm(
  props: Props
): ReactElement {
  return (
    <OptionsForm
      {...props}
      schema={GenerateE164PhoneNumberSchema}
      options={[
        {
          field: 'min',
          label: 'Minimum Length',
          description:
            'Set the minimum length range of the output phone number. It cannot be less than 9.',
        },
        {
          field: 'max',
          label: 'Maximum Length',
          description:
            'Set the maximum length range of the output phone number. It cannot be greater than 15.',
        },
      ]}
    />
  );
}
