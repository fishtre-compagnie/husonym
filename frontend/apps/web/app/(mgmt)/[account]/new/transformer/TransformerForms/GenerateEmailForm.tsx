'use client';
import { getGenerateEmailTypeString } from '@/util/util';
import { GenerateEmail, GenerateEmailSchema } from '@husonym/sdk';
import { ReactElement } from 'react';
import OptionsForm from './options/OptionsForm';
import { TransformerConfigProps } from './util';

interface Props extends TransformerConfigProps<GenerateEmail> {}

export default function GenerateEmailForm(props: Props): ReactElement {
  return (
    <OptionsForm
      {...props}
      schema={GenerateEmailSchema}
      options={[
        {
          field: 'emailType',
          label: 'Email Type',
          description:
            'Select the type of email you want to generate. Uuid_v4 emails guarantee uniqueness.',
          formatEnumValue: getGenerateEmailTypeString,
        },
      ]}
    />
  );
}
