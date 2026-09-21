import Spinner from '@/components/Spinner';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { ScrollArea } from '@/components/ui/scroll-area';
import {
  CheckCircledIcon,
  CheckIcon,
  ExclamationTriangleIcon,
  ReloadIcon,
} from '@radix-ui/react-icons';
import { ReactElement } from 'react';
import AcceptPassthroughButton, {
  PassthroughTarget,
} from './AcceptPassthroughButton';

export type ErrorLevel = 'error' | 'warning';

export interface FormError {
  message: string;
  type?: string;
  path: string;
  level: ErrorLevel;
  // Set on the warnings about a column the job copies untransformed while it waits for a
  // decision. Carries the column in pieces because the decision is written against it, and
  // splitting `path` back apart would break on any name holding a dot.
  acceptable?: PassthroughTarget;
}

interface Props {
  formErrors: FormError[];
  isValidating?: boolean;
  onValidate?(): void;
  // Absent while a job is being created: there is no job yet to record a decision against.
  onAcceptPassthrough?(target: PassthroughTarget, note?: string): Promise<void>;
}

export default function FormErrorsCard(props: Props): ReactElement {
  const { formErrors, isValidating, onValidate, onAcceptPassthrough } = props;

  const { errors, warnings } = formErrorsToMessages(formErrors);
  return (
    <Card className="w-full flex flex-col">
      <CardHeader className="flex flex-col">
        <div className="flex flex-row items-center justify-between h-8">
          <div className="flex flex-row items-center gap-2">
            {errors.length != 0 ? (
              <ExclamationTriangleIcon className="h-4 w-4 text-destructive dark:text-red-400 text-red-600" />
            ) : (
              <CheckCircledIcon className="w-4 h-4" />
            )}
            <CardTitle>Validations</CardTitle>
            {errors.length != 0 && (
              <Badge variant="destructive">
                {errors.length == 1
                  ? `${errors.length} Error`
                  : `${errors.length} Errors`}
              </Badge>
            )}
            {warnings.length != 0 && (
              <Badge className="bg-yellow-200 dark:bg-yellow-800/70 text-yellow-900 dark:text-yellow-200">
                {warnings.length == 1
                  ? `${warnings.length} Warning`
                  : `${warnings.length} Warnings`}
              </Badge>
            )}
          </div>
          <div className="flex">
            {onValidate && (
              <Button
                variant="ghost"
                className="h-4 w-4"
                size="icon"
                key="validate"
                type="button"
              >
                {isValidating ? (
                  <Spinner className="h-4 w-4" />
                ) : (
                  <ReloadIcon
                    className="h-4 w-4"
                    onClick={() => onValidate()}
                  />
                )}
              </Button>
            )}
          </div>
        </div>
        <CardDescription>
          A list of schema validation errors to resolve before moving forward.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col flex-1">
        {formErrors.length === 0 && warnings.length === 0 ? (
          <div className="flex flex-col flex-1 items-center justify-center bg-green-100 dark:bg-green-900 text-green-900 dark:text-green-200 rounded-xl">
            <div className="text-sm flex flex-row items-center gap-2 px-1">
              <div className="flex">
                <CheckIcon />
              </div>
              <p>Everything looks good!</p>
            </div>
          </div>
        ) : (
          <ScrollArea className="max-h-[177px] overflow-auto">
            <div className="flex flex-col gap-2">
              {errors.map((entry, index) => (
                <div
                  key={entry.message + index}
                  className="text-xs bg-red-200 dark:bg-red-800/70 rounded-sm p-2 text-wrap"
                >
                  {entry.message}
                </div>
              ))}
              {warnings.map((entry, index) => (
                <div
                  key={entry.message + index}
                  className="text-xs bg-yellow-200 dark:bg-yellow-800/70 rounded-sm p-2 text-wrap flex flex-row items-start justify-between gap-2"
                >
                  <span>{entry.message}</span>
                  {entry.acceptable && onAcceptPassthrough && (
                    <AcceptPassthroughButton
                      target={entry.acceptable}
                      onAccept={onAcceptPassthrough}
                    />
                  )}
                </div>
              ))}
            </div>
          </ScrollArea>
        )}
      </CardContent>
    </Card>
  );
}

// A line ready to render: the sentence, plus what is needed to act on it. The message used to
// be a bare string, which left nowhere to hang an action.
interface FormErrorMessage {
  message: string;
  acceptable?: PassthroughTarget;
}

interface FormErrorMesageResponse {
  errors: FormErrorMessage[];
  warnings: FormErrorMessage[];
}

function formErrorsToMessages(
  formErrors: FormError[]
): FormErrorMesageResponse {
  const errors: FormErrorMessage[] = [];
  const warnings: FormErrorMessage[] = [];
  formErrors.forEach((error) => {
    const pieces: string[] = [error.path];
    if (error.type) {
      pieces.push(`[${error.type}]`);
    }
    pieces.push(error.message);
    const entry: FormErrorMessage = {
      message: pieces.join(' '),
      acceptable: error.acceptable,
    };

    if (error.level == 'warning') {
      warnings.push(entry);
      return;
    }
    errors.push(entry);
  });

  return { errors, warnings };
}
