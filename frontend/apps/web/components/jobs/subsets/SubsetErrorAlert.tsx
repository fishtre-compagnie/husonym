import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { CheckSqlQueryResponse } from '@husonym/sdk';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';

interface Props {
  validateResp?: CheckSqlQueryResponse;
  rowCountError?: string;
}

export default function ValidateQueryErrorAlert(
  props: Props
): ReactElement | null {
  const { validateResp, rowCountError } = props;
  const t = useTranslations('mapping.subsets.errorAlert');

  const hasSqlError =
    validateResp && !validateResp.isValid && validateResp.erorrMessage;
  const hasRowCountError = rowCountError && rowCountError.trim() !== '';

  if (!hasSqlError && !hasRowCountError) {
    return null;
  }

  return (
    <div className="flex flex-col gap-2">
      {!validateResp?.isValid && (
        <Alert variant="destructive">
          <AlertTitle>{t('invalidSql')}</AlertTitle>
          <AlertDescription>
            {validateResp?.erorrMessage
              ? validateResp?.erorrMessage
              : t('unknownError')}
          </AlertDescription>
        </Alert>
      )}
      {rowCountError && (
        <Alert variant="destructive">
          <AlertTitle>{t('rowCountFailed')}</AlertTitle>
          <AlertDescription>
            {rowCountError ? rowCountError : t('unknownError')}
          </AlertDescription>
        </Alert>
      )}
    </div>
  );
}
