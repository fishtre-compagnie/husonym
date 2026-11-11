import { cn } from '@/libs/utils';
import { ReactElement } from 'react';
import { FormStep, FormStepName } from './WelcomeDialog';

interface Props {
  steps: FormStep[];
  currentStep: FormStepName;
  stepOrder: FormStepName[];
}

export default function StepProgress(props: Props): ReactElement {
  const { steps, currentStep, stepOrder } = props;

  return (
    <div className="flex flex-row items-center gap-1 justify-center">
      {steps.map((step) => (
        <div
          key={step.name}
          className={cn(
            'rounded-2xl w-2 h-2',
            stepOrder.indexOf(step.name) <= stepOrder.indexOf(currentStep)
              ? 'bg-primary'
              : 'bg-surface-2' // Upcoming steps
          )}
        ></div>
      ))}
    </div>
  );
}
