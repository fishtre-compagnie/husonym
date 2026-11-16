import ButtonText from '@/components/ButtonText';
import ConfirmationDialog from '@/components/ConfirmationDialog';
import SwitchCard from '@/components/switches/SwitchCard';
import { Button } from '@/components/ui/button';
import { useFormatter, useTranslations } from 'next-intl';
import { ReactElement, useState } from 'react';

interface Props {
  onClick(shouldFormat: boolean): void | Promise<void>;
  count?: number;
}

export default function ExportJobMappingsButton(props: Props): ReactElement {
  const { onClick, count } = props;
  const [prettyPrint, setPrettyPrint] = useState<boolean>(false);
  const t = useTranslations('mapping.schemaTable.exportMappings');
  const headerText = useHeaderText(count);
  return (
    <div>
      <ConfirmationDialog
        trigger={
          <Button type="button" variant="outline">
            <ButtonText text={t('export')} />
          </Button>
        }
        headerText={headerText}
        description={t('description')}
        body={
          <Body prettyPrint={prettyPrint} setPrettyPrint={setPrettyPrint} />
        }
        containerClassName="max-w-xl"
        buttonText={t('confirm')}
        onConfirm={() => {
          // onClick can be a promise, but we don't necessarly want to wait for it
          // as it might take a long time
          onClick(prettyPrint);
          if (prettyPrint) {
            setPrettyPrint(false);
          }
        }}
      />
    </div>
  );
}

function useHeaderText(count?: number): string {
  const t = useTranslations('mapping.schemaTable.exportMappings');
  const format = useFormatter();
  if (!count) {
    return t('headerAll');
  }
  return t('headerSelected', { count: format.number(count) });
}

interface BodyProps {
  prettyPrint: boolean;
  setPrettyPrint(value: boolean): void;
}

function Body(props: BodyProps): ReactElement {
  const { prettyPrint, setPrettyPrint } = props;
  const t = useTranslations('mapping.schemaTable.exportMappings');

  return (
    <div className="flex flex-col gap-2">
      <SwitchCard
        isChecked={prettyPrint}
        onCheckedChange={setPrettyPrint}
        title={t('formatTitle')}
        description={t('formatDescription')}
      />
    </div>
  );
}
