'use client';
import { Pill } from '@/components/ds';
import LicenseFeatureNotice from '@/components/LicenseFeatureNotice';
import { SelectCard, WizardFooter } from '@/components/progress-steps/Wizard';
import { useAccount } from '@/components/providers/account-provider';
import { PageProps } from '@/components/types';
import { Button } from '@/components/ui/button';
import { useGetSystemAppConfig } from '@/libs/hooks/useGetSystemAppConfig';
import { useLicenseFeature } from '@/libs/hooks/useLicense';
import { MagicWandIcon, SymbolIcon } from '@radix-ui/react-icons';
import { nanoid } from 'nanoid';
import {
  ReadonlyURLSearchParams,
  useRouter,
  useSearchParams,
} from 'next/navigation';
import { ReactElement, ReactNode, use, useState } from 'react';
import { AiOutlineExperiment } from 'react-icons/ai';
import { PiDetective } from 'react-icons/pi';
import {
  getJobProgressStepLabels,
  getJobProgressSteps,
} from './JobsProgressSteps';
import { NewJobType } from './job-form-validations';
import NewJobWizard from './NewJobWizard';

export default function NewJob(props: PageProps): ReactElement {
  const params = use(props.params);
  const searchParams = useSearchParams();
  const { account } = useAccount();

  const jobData = useGetJobData(params, searchParams);

  const [selectedJobType, setSelectedJobType] = useState<NewJobType>(
    jobData[0].type
  );

  const [href, setHref] = useState<string | undefined>(jobData[0].href);

  const { allowed: isPiiDetectionAllowed } = useLicenseFeature('pii_detection');
  // The job types the license does not include are shown, but cannot be picked.
  const isLocked = (jobType: NewJobType) =>
    jobType === 'pii-detection' && !isPiiDetectionAllowed;

  const handleJobSelection = (jobType: NewJobType, href: string) => {
    setSelectedJobType(jobType);
    setHref(href);
  };

  const router = useRouter();
  const selected = jobData.find((jd) => jd.type === selectedJobType);

  return (
    <NewJobWizard
      id="newjobdefine"
      steps={getJobProgressSteps(selectedJobType)}
      stepLabels={getJobProgressStepLabels(selectedJobType)}
      stepName="type"
      title="What do you want to do?"
      description="Pick the kind of job. The next steps follow from it: a name, the connections, then what happens to each column."
      mascot
    >
      <div
        role="radiogroup"
        aria-label="Job type"
        className="grid grid-cols-[repeat(auto-fit,minmax(220px,1fr))] gap-2.5"
      >
        {jobData.map((jd) => {
          const locked = isLocked(jd.type);
          return (
            <SelectCard
              key={jd.type}
              id={jd.type}
              selected={selectedJobType === jd.type}
              disabled={locked}
              onClick={() => {
                if (!locked) {
                  handleJobSelection(jd.type, jd.href);
                }
              }}
              className="flex-col gap-2 p-4"
            >
              <span className="flex flex-wrap items-center gap-2">
                <span
                  aria-hidden="true"
                  className="grid size-8 place-items-center rounded-lg bg-surface-2 text-link"
                >
                  {jd.icon}
                </span>
                <span className="text-[15px] font-semibold">{jd.name}</span>
                {jd.experimental && <Pill tone="quiet">Experimental</Pill>}
              </span>
              <span className="text-[13px] leading-[1.45] text-muted-foreground">
                {jd.description}
              </span>
            </SelectCard>
          );
        })}
      </div>
      {jobData.some((jd) => jd.type === 'pii-detection') && (
        <LicenseFeatureNotice feature="pii_detection" />
      )}
      <WizardFooter summary={selected ? [selected.name] : []}>
        <Button
          variant="outline"
          type="reset"
          onClick={() => router.push(`/${account?.name}`)}
        >
          Back
        </Button>
        <Button
          type="submit"
          disabled={!selectedJobType || !href}
          onClick={() => {
            if (!href) {
              return;
            }
            router.push(href);
          }}
        >
          Continue
        </Button>
      </WizardFooter>
    </NewJobWizard>
  );
}

interface JobData {
  name: string;
  description: string;
  href: string;
  icon: ReactNode;
  type: NewJobType;
  experimental: boolean;
}

function useGetJobData(
  params: Record<string, string>,
  searchParams: ReadonlyURLSearchParams
): JobData[] {
  const [sessionToken] = useState<string>(params?.sessionId ?? nanoid());
  const { account } = useAccount();
  const { data: systemAppConfig } = useGetSystemAppConfig();

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

  const piiDetectionParams = new URLSearchParams(searchParams);
  piiDetectionParams.set('jobType', 'pii-detection');
  if (!piiDetectionParams.has('sessionId')) {
    piiDetectionParams.set('sessionId', sessionToken);
  }
  const jobData: JobData[] = [
    {
      name: 'Copy and anonymize',
      description:
        'Real data, transformed column by column, written to a test database.',
      href: `/${account?.name}/new/job/define?${dataSyncParams.toString()}`,
      icon: <SymbolIcon />,
      type: 'data-sync',
      experimental: false,
    },
    {
      name: 'Generate data',
      description:
        'Fully synthetic rows for a chosen destination, from the schema only.',
      href: `/${account?.name}/new/job/define?${dataGenParams.toString()}`,
      icon: <AiOutlineExperiment />,
      type: 'generate-table',
      experimental: false,
    },
    {
      name: 'Generate with AI',
      description:
        'Synthetic rows written by a model, shaped by a table schema and your prompt.',
      href: `/${account?.name}/new/job/define?${aiDataGenParams.toString()}`,
      icon: <MagicWandIcon />,
      type: 'ai-generate-table',
      experimental: true,
    },
  ];

  if (systemAppConfig?.isPiiDetectionJobEnabled) {
    jobData.push({
      name: 'Find personal data',
      description:
        'A report of the sensitive columns in a database, without copying anything.',
      href: `/${account?.name}/new/job/define?${piiDetectionParams.toString()}`,
      icon: <PiDetective />,
      type: 'pii-detection',
      experimental: true,
    });
  }

  return jobData;
}
