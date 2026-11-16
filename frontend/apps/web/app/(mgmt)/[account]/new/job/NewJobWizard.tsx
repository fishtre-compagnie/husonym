'use client';
import { WizardFrame, WizardHeading } from '@/components/progress-steps/Wizard';
import { useAccount } from '@/components/providers/account-provider';
import { useTranslations } from 'next-intl';
import { ReactElement, ReactNode } from 'react';
import JobsProgressSteps, {
  JobProgressStep,
  getJobProgressStepLabels,
  getStepPosition,
} from './JobsProgressSteps';

interface Props {
  /** The steps of the current flow, as getJobProgressSteps gives them. */
  steps: JobProgressStep[];
  stepName: JobProgressStep;
  stepLabels?: ReturnType<typeof getJobProgressStepLabels>;
  title: ReactNode;
  description?: ReactNode;
  mascot?: boolean;
  headingActions?: ReactNode;
  wide?: boolean;
  id?: string;
  children: ReactNode;
}

/**
 * A page of the job creation wizard: the way out, the steps of the flow, then
 * the step's question and its content. The content ends with a WizardFooter.
 */
export default function NewJobWizard(props: Props): ReactElement {
  const {
    steps,
    stepName,
    stepLabels,
    title,
    description,
    mascot,
    headingActions,
    wide,
    id,
    children,
  } = props;
  const { account } = useAccount();
  const t = useTranslations('newJob.wizard');
  return (
    <WizardFrame
      id={id}
      title={t('title')}
      exitHref={`/${account?.name ?? ''}/jobs`}
      exitLabel={t('exit')}
      wide={wide}
      steps={
        <JobsProgressSteps
          steps={steps}
          stepName={stepName}
          labels={stepLabels}
        />
      }
    >
      <WizardHeading
        position={getStepPosition(steps, stepName)}
        title={title}
        description={description}
        mascot={mascot}
        actions={headingActions}
      />
      {children}
    </WizardFrame>
  );
}
