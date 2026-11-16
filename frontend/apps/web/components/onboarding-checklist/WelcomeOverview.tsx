import { ArrowRightIcon } from '@radix-ui/react-icons';
import { useTranslations } from 'next-intl';
import { useTheme } from 'next-themes';
import { ReactElement } from 'react';
import { Button } from '../ui/button';
import { ConnectDarkMode } from './ConnectDarkMode';
import { ConnectLightMode } from './ConnectLightMode';

interface Props {
  onNextStep: () => void;
  setIsDialogOpen: (val: boolean) => void;
  completeForm: () => Promise<void>;
}

export default function WelcomeOverview(props: Props): ReactElement {
  const { onNextStep, setIsDialogOpen, completeForm } = props;
  const theme = useTheme();
  const t = useTranslations('dashboard.onboarding');

  return (
    <div className="flex flex-col gap-12 justify-center items-center text-center">
      <h1 className="font-semibold text-2xl">{t('welcome.title')}</h1>
      {theme.resolvedTheme == 'light' ? (
        <ConnectLightMode />
      ) : (
        <ConnectDarkMode />
      )}
      <p className="text-sm px-12">
        {t.rich('welcome.text', {
          b: (chunks) => <span className="font-semibold">{chunks}</span>,
        })}
      </p>
      <div className="flex flex-row justify-between w-full py-6">
        <Button
          type="button"
          variant="outline"
          onClick={() => {
            completeForm();
            setIsDialogOpen(false);
          }}
        >
          {t('welcome.skip')}
        </Button>
        <Button onClick={onNextStep}>
          <div className="flex flex-row items-center gap-2">
            <div>{t('next')}</div> <ArrowRightIcon />
          </div>
        </Button>
      </div>
    </div>
  );
}
