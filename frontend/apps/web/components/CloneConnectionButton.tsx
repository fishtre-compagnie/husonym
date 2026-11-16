import NextLink from 'next/link';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import { GrClone } from 'react-icons/gr';
import ButtonText from './ButtonText';
import { useAccount } from './providers/account-provider';
import { Button } from './ui/button';

interface CloneConnectionProps {
  id: string;
}

export function CloneConnectionButton(
  props: CloneConnectionProps
): ReactElement {
  const { id } = props;
  const t = useTranslations('connections.detail');
  const { account } = useAccount();

  return (
    <NextLink href={`/${account?.name}/connections/${id}/clone`}>
      <Button>
        <ButtonText text={t('clone')} leftIcon={<GrClone className="mr-1" />} />
      </Button>
    </NextLink>
  );
}
