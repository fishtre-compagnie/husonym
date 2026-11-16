import { NewJobType } from '@/app/(mgmt)/[account]/new/job/job-form-validations';
import { useAccount } from '@/components/providers/account-provider';
import { Badge } from '@/components/ui/badge';
import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group';
import { cn } from '@/libs/utils';
import {
  ArrowRightIcon,
  MagicWandIcon,
  SymbolIcon,
} from '@radix-ui/react-icons';
import { nanoid } from 'nanoid';
import { useTranslations } from 'next-intl';
import { useRouter, useSearchParams } from 'next/navigation';
import { ReactElement, useEffect, useState } from 'react';
import { AiOutlineExperiment } from 'react-icons/ai';
import { Button } from '../ui/button';

interface Props {
  onPreviousStep: () => void;
  setIsDialogOpen: (val: boolean) => void;
  completeForm: () => Promise<void>;
}

export default function WelcomeRouter({
  setIsDialogOpen,
  onPreviousStep,
  completeForm,
}: Props): ReactElement {
  const [sessionToken, setSessionToken] = useState<string>('');
  const searchParams = useSearchParams();
  const { account } = useAccount();
  const t = useTranslations('dashboard.onboarding');

  useEffect(() => {
    // Generate the session token only on the client side
    setSessionToken(searchParams.get('sessionId') ?? nanoid());
  }, []);

  const dataSyncParams = new URLSearchParams(searchParams);
  dataSyncParams.set('jobType', 'data-sync');
  if (!dataSyncParams.has('sessionId')) {
    dataSyncParams.set('sessionId', sessionToken);
  }

  const dataGenParams = new URLSearchParams(searchParams);
  dataGenParams.set('jobType', 'generate-table');
  if (!dataGenParams.has('sessionId')) {
    dataGenParams.set('sessionId', sessionToken);
  }

  const aiDataGenParams = new URLSearchParams(searchParams);
  aiDataGenParams.set('jobType', 'ai-generate-table');
  if (!aiDataGenParams.has('sessionId')) {
    aiDataGenParams.set('sessionId', sessionToken);
  }

  const jobData = [
    {
      name: t('router.dataSync'),
      description: t('router.dataSyncDescription'),
      href: `/${account?.name}/new/job/define?${dataSyncParams.toString()}`,
      icon: <SymbolIcon />,
      type: 'data-sync',
      experimental: false,
    },
    {
      name: t('router.dataGen'),
      description: t('router.dataGenDescription'),
      href: `/${account?.name}/new/job/define?${dataGenParams.toString()}`,
      icon: <AiOutlineExperiment />,
      type: 'generate-table',
      experimental: false,
    },
    {
      name: t('router.aiDataGen'),
      description: t('router.aiDataGenDescription'),
      href: `/${account?.name}/new/job/define?${aiDataGenParams.toString()}`,
      icon: <MagicWandIcon />,
      type: 'ai-generate-table',
      experimental: true,
    },
  ] as const;

  const [selectedJobType, setSelectedJobType] =
    useState<NewJobType>('data-sync');

  const [href, setHref] = useState<string>();

  const handleJobSelection = (jobType: NewJobType, href: string) => {
    setSelectedJobType(jobType);
    setHref(href);
  };

  const router = useRouter();

  return (
    <div className="flex flex-col gap-8 justify-center items-center text-center ">
      <h1 className="font-semibold text-2xl">{t('router.title')}</h1>
      <p className="text-sm px-10">{t('router.text')}</p>
      <div className="flex flex-col justify-center">
        <RadioGroup value={selectedJobType} onChange={() => setSelectedJobType}>
          {jobData.map((jd) => (
            <Card
              key={jd.name}
              className={cn(
                'cursor-pointer p-2',
                selectedJobType === jd.type
                  ? 'border border-primary bg-brand-soft'
                  : 'border border-border hover:bg-hover'
              )}
              onClick={() => handleJobSelection(jd.type, jd.href)}
            >
              <CardHeader>
                <div className="flex flex-col md:flex-row justify-between items-center">
                  <div>
                    <CardTitle>
                      <div className="flex flex-row items-center gap-2">
                        <div>{jd.icon}</div>
                        <p>{jd.name}</p>
                        {jd.experimental ? (
                          <Badge>{t('router.experimental')}</Badge>
                        ) : null}
                      </div>
                    </CardTitle>
                    <CardDescription className="pl-6 pt-2">
                      {jd.description}
                    </CardDescription>
                  </div>
                  <RadioGroupItem
                    value={jd.type}
                    id={jd.type}
                    className={
                      selectedJobType === jd.type
                        ? 'bg-primary text-primary-foreground'
                        : 'bg-card text-foreground'
                    }
                  />
                </div>
              </CardHeader>
            </Card>
          ))}
        </RadioGroup>
      </div>
      <div className="flex flex-row justify-between w-full py-6">
        <Button variant="outline" type="reset" onClick={onPreviousStep}>
          {t('back')}
        </Button>
        <Button
          type="submit"
          disabled={!selectedJobType}
          onClick={() => {
            setIsDialogOpen(false);
            completeForm();
            onPreviousStep;
            router.push(
              href ??
                `/${account?.name}/new/job/define?${dataSyncParams.toString()}`
            );
          }}
        >
          <div className="flex flex-row items-center gap-2">
            <div>{t('next')}</div> <ArrowRightIcon />
          </div>
        </Button>
      </div>
    </div>
  );
}
