'use client';
import {
  getGenerateEmailTypeString,
  getInvalidEmailActionString,
} from '@/util/util';
import { TransformEmail, TransformEmailSchema } from '@husonym/sdk';
import { ReactElement } from 'react';
import OptionsForm from './options/OptionsForm';
import { TransformerConfigProps } from './util';

interface Props extends TransformerConfigProps<TransformEmail> {}

export default function TransformEmailForm(props: Props): ReactElement {
  return (
    <OptionsForm
      {...props}
      schema={TransformEmailSchema}
      options={[
        {
          field: 'preserveLength',
          label: 'Preserve Length',
          description:
            'Set the length of the output email to be the same as the input',
        },
        {
          field: 'preserveDomain',
          label: 'Preserve Domain',
          description:
            'Preserve the input domain including top level domain to the output value. For ex. if the input is john@gmail.com, the output will be ij23o@gmail.com',
        },
        {
          field: 'excludedDomains',
          label: 'Excluded Domains',
          description:
            'Provide a list of comma-separated domains that you want to be excluded from the transformer. Do not provide an @ with the domains.',
        },
        {
          field: 'emailType',
          label: 'Email Type',
          description:
            'Configure the email type that will be used during transformation.',
          formatEnumValue: getGenerateEmailTypeString,
        },
        {
          field: 'invalidEmailAction',
          label: 'Invalid Email Action',
          description:
            'Configure the invalid email action that will be run in the event the system encounters an email that does not conform to RFC 5322.',
          formatEnumValue: getInvalidEmailActionString,
        },
      ]}
    />
  );
}
