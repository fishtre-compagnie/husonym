'use client';
import { getGenerateIpAddressVersionString } from '@/util/util';
import { GenerateIpAddress, GenerateIpAddressSchema } from '@husonym/sdk';
import { ReactElement } from 'react';
import OptionsForm from './options/OptionsForm';
import { TransformerConfigProps } from './util';

interface Props extends TransformerConfigProps<GenerateIpAddress> {}

export default function GenerateIpAddressForm(props: Props): ReactElement {
  return (
    <OptionsForm
      {...props}
      schema={GenerateIpAddressSchema}
      options={[
        {
          field: 'ipType',
          label: 'IP Version',
          description:
            'Select if you want to generate an IPv4 or IPv6 address.',
          formatEnumValue: getGenerateIpAddressVersionString,
        },
      ]}
    />
  );
}
