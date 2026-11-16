import { Badge } from '@/components/ui/badge';
import { CheckSqlQueryResponse } from '@husonym/sdk';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';

interface Props {
  resp?: CheckSqlQueryResponse;
}

export default function ValidateQueryBadge(props: Props): ReactElement | null {
  const { resp } = props;
  const t = useTranslations('mapping.subsets.validateBadge');
  if (!resp) {
    return null;
  }
  const text = resp.isValid ? t('valid') : t('invalid');
  return (
    <Badge
      variant={resp.isValid ? 'success' : 'destructive'}
      className="cursor-default px-4 py-2 h-6"
    >
      {text}
    </Badge>
  );
}
