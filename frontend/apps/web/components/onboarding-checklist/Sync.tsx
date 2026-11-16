import { ArrowRightIcon, CheckIcon } from '@radix-ui/react-icons';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import { Badge } from '../ui/badge';
import { Button } from '../ui/button';
import { Input } from '../ui/input';

interface Props {
  onNextStep: () => void;
  onPreviousStep: () => void;
}

export default function Sync(props: Props): ReactElement {
  const { onNextStep, onPreviousStep } = props;
  const t = useTranslations('dashboard.onboarding');

  return (
    <div className="flex flex-col gap-12 justify-center items-center text-center">
      <h1 className="font-semibold text-2xl">{t('sync.title')}</h1>
      <p className="text-sm px-10">
        {t.rich('sync.text', {
          b: (chunks) => <span className="font-semibold">{chunks}</span>,
        })}
      </p>
      <div className=" flex flex-col gap-8">
        <div className=" flex flex-col gap-2 text-left">
          <p className="text-sm font-semibold">{t('sync.schedule')}</p>
          <Input
            disabled={true}
            value="0 0 1 1 *"
            className="w-[200px] border border-border"
          />
        </div>
        <div className=" flex flex-col gap-2 text-left">
          <p className="text-sm font-semibold">{t('sync.jobRuns')}</p>
          <div className="flex flex-col gap-4 rounded-xl border border-border bg-card p-4 text-xs">
            <div className="flex flex-row gap-4 justify-between" id="header">
              <div className="w-[100px] font-semibold">{t('sync.job')}</div>
              <div className="w-[100px] font-semibold">{t('sync.start')}</div>
              <div className="w-[100px] font-semibold">
                {t('sync.complete')}
              </div>
              <div className="w-[100px] font-semibold">{t('sync.status')}</div>
            </div>
            <div className="flex flex-row items-center gap-4 justify-between">
              <div className="w-[100px]">sync-prod</div>
              <div className="w-[100px]">08/14/2024 12:15:54 PM</div>
              <div className="w-[100px]">08/14/2024 12:16:00 PM</div>
              <Badge variant="success" className="w-[100px]">
                <CheckIcon className="w-3 h-3" />{' '}
                <p>{t('sync.completeStatus')}</p>
              </Badge>
            </div>
          </div>
        </div>
      </div>
      <div className="flex flex-row justify-between w-full py-6">
        <Button variant="outline" type="reset" onClick={onPreviousStep}>
          {t('back')}
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
