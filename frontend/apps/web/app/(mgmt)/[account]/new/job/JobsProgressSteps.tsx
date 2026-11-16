import Step from '@/components/progress-steps/Step';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import { NewJobType } from './job-form-validations';

const DATA_SYNC_STEPS_NO_SUBSET: JobProgressStep[] = [
  'define',
  'connect',
  'schema',
];
const DATA_SYNC_STEPS_WITH_SUBSET: JobProgressStep[] = [
  'define',
  'connect',
  'schema',
  'subset',
];

const DATA_GEN_STEPS: JobProgressStep[] = ['define', 'connect', 'schema'];

const PII_DETECTION_STEPS: JobProgressStep[] = ['define', 'connect', 'schema'];

/** `type` is the choice of the kind of job, the page every flow starts on. */
export type JobProgressStep =
  'type' | 'define' | 'connect' | 'schema' | 'subset';

/** A step's label, as a key of the `newJob.steps` messages. */
type StepLabelKey = JobProgressStep | 'source' | 'scan' | 'schemaOnly';

const STEP_LABELS: Record<JobProgressStep, StepLabelKey> = {
  type: 'type',
  define: 'define',
  connect: 'connect',
  schema: 'schema',
  subset: 'subset',
};

interface Props {
  steps: JobProgressStep[];
  stepName: JobProgressStep;
  /** Overrides of a step's label, for a flow that names a step its own way. */
  labels?: Partial<Record<JobProgressStep, StepLabelKey>>;
}

/** The steps a flow shows, with the job type choice always first. */
function withTypeStep(steps: JobProgressStep[]): JobProgressStep[] {
  return steps[0] === 'type' ? steps : ['type', ...steps];
}

/** Where a step sits in its flow, counting the job type choice. */
export function getStepPosition(
  steps: JobProgressStep[],
  stepName: JobProgressStep
): { index: number; total: number } {
  const all = withTypeStep(steps);
  return { index: all.indexOf(stepName) + 1, total: all.length };
}

export default function JobsProgressSteps(props: Props): ReactElement {
  const { stepName, labels } = props;
  const t = useTranslations('newJob.steps');
  const steps = withTypeStep(props.steps);

  const currentStepIndex = steps.indexOf(stepName);

  return (
    <ol
      aria-label={t('label')}
      className="m-0 flex list-none flex-wrap gap-1.5 p-0"
    >
      {steps.map((step, index) => (
        <Step
          key={step}
          name={step}
          label={t(labels?.[step] ?? STEP_LABELS[step])}
          position={index + 1}
          isCompleted={index < currentStepIndex}
          isActive={step === stepName}
          isLastStep={index === steps.length - 1}
        />
      ))}
    </ol>
  );
}

/** How a flow names its steps, where it differs from the defaults. */
export function getJobProgressStepLabels(
  jobtype: NewJobType
): Partial<Record<JobProgressStep, StepLabelKey>> {
  switch (jobtype) {
    case 'generate-table':
    case 'ai-generate-table':
      return { schema: 'schemaOnly' };
    case 'pii-detection':
      return { connect: 'source', schema: 'scan' };
    default:
      return {};
  }
}

export function getJobProgressSteps(
  jobtype: 'data-sync',
  includeSubsetting: boolean
): JobProgressStep[];
export function getJobProgressSteps(
  jobtype: 'ai-generate-table' | 'generate-table' | 'pii-detection'
): JobProgressStep[];
export function getJobProgressSteps(jobtype: NewJobType): JobProgressStep[];
export function getJobProgressSteps(
  jobtype: NewJobType,
  includeSubsetting?: boolean
): JobProgressStep[] {
  switch (jobtype) {
    case 'data-sync':
      return includeSubsetting
        ? DATA_SYNC_STEPS_WITH_SUBSET
        : DATA_SYNC_STEPS_NO_SUBSET;
    case 'generate-table':
      return DATA_GEN_STEPS;
    case 'ai-generate-table':
      return DATA_GEN_STEPS;
    case 'pii-detection':
      return PII_DETECTION_STEPS;
    default:
      return [];
  }
}
