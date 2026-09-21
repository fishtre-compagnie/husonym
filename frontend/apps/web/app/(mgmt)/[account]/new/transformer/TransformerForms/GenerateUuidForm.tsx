'use client';
import { GenerateUuid, GenerateUuidSchema } from '@husonym/sdk';
import { ReactElement } from 'react';
import OptionsForm from './options/OptionsForm';
import { TransformerConfigProps } from './util';

interface Props extends TransformerConfigProps<GenerateUuid> {}

export default function GenerateUuidForm(props: Props): ReactElement {
  return (
    <OptionsForm
      {...props}
      schema={GenerateUuidSchema}
      options={[
        {
          field: 'includeHyphens',
          label: 'Include hyphens',
          description:
            'Set to true to include hyphens in the generated UUID. Note: some databases such as Postgres automatically convert UUIDs with no hyphens to have hyphens when they store the data.',
        },
      ]}
    />
  );
}
