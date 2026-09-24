'use client';
import { useCheckedSave } from '@/components/connections/checks/useCheckedSave';
import ConnectionSelectContent from '@/app/(mgmt)/[account]/new/job/connect/ConnectionSelectContent';
import ButtonText from '@/components/ButtonText';
import DeleteConfirmationDialog from '@/components/DeleteConfirmationDialog';
import DestinationOptionsForm from '@/components/jobs/Form/DestinationOptionsForm';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardFooter } from '@/components/ui/card';
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
} from '@/components/ui/form';
import {
  Select,
  SelectContent,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { splitConnections } from '@/libs/utils';
import { getErrorMessage } from '@/util/util';
import { NewDestinationFormValues } from '@/yup-validations/jobs';
import { useMutation } from '@connectrpc/connect-query';
import { yupResolver } from '@/util/yup-form-resolver';
import {
  Connection,
  JobDestination,
  JobMapping,
  Job,
  JobDestinationOptions,
  JobService,
} from '@husonym/sdk';
import { TrashIcon } from '@radix-ui/react-icons';
import { ReactElement } from 'react';
import { Control, useForm, useWatch } from 'react-hook-form';
import { toast } from 'sonner';
import {
  getDestinationFormValuesOrDefaultFromDestination,
  toJobDestinationOptions,
} from '../../../util';
import DestinationAlerts from './DestinationAlerts';
interface Props {
  jobId: string;
  jobSourceId: string;
  destination: JobDestination;
  connections: Connection[];
  availableConnections: Connection[];
  mutate: () => {};
  isDeleteDisabled?: boolean;
  jobmappings?: JobMapping[];
  // The job the destination belongs to: what it asks of the destination is checked before
  // the destination is saved.
  job?: Job;
}

export default function DestinationConnectionCard({
  jobId,
  destination,
  connections,
  availableConnections,
  mutate,
  isDeleteDisabled,
  jobSourceId,
  jobmappings,
  job,
}: Props): ReactElement {
  const { mutateAsync: setJobDestConnection } = useMutation(
    JobService.method.updateJobDestinationConnection
  );
  const { mutateAsync: removeJobDestConnection } = useMutation(
    JobService.method.deleteJobDestinationConnection
  );
  const { checkThenSave, dialog: checksDialog } = useCheckedSave();

  const form = useForm({
    resolver: yupResolver(NewDestinationFormValues),
    values: getDestinationFormValuesOrDefaultFromDestination(destination),
  });

  async function onSubmit(values: NewDestinationFormValues) {
    const connection = connections.find((c) => c.id === values.connectionId);
    const options = toJobDestinationOptions(values, connection);
    if (!job) {
      return;
    }
    // Only this destination is checked: the others and the source are not changed.
    await checkThenSave(
      {
        ...job,
        destinations: [{ connectionId: values.connectionId, options }],
      },
      () => saveDestination(values.connectionId, options),
      { checkSource: false }
    );
  }

  async function saveDestination(
    connectionId: string,
    options: JobDestinationOptions
  ): Promise<void> {
    try {
      await setJobDestConnection({
        jobId,
        connectionId,
        destinationId: destination.id,
        options,
      });
      mutate();
      toast.success('Successfully updated job destination!');
    } catch (err) {
      console.error(err);
      toast.error('Unable to update job destination', {
        description: getErrorMessage(err),
      });
    }
  }

  async function onDelete() {
    try {
      await removeJobDestConnection({
        destinationId: destination.id,
      });
      mutate();
      toast.success('Successfully deleted job destination!');
    } catch (err) {
      console.error(err);
      toast.error('Unable to delete job destination', {
        description: getErrorMessage(err),
      });
    }
  }

  const dest = availableConnections.find(
    (item) => item.id === destination.connectionId
  );
  const destOpts = form.watch('destinationOptions');
  const shouldHideInitTableSchema = useShouldHideInitConnectionSchema(
    form.control,
    jobSourceId
  );

  const { postgres, mysql, s3, mongodb, gcpcs, dynamodb, mssql } =
    splitConnections(availableConnections);
  return (
    <Card>
      <Form {...form}>
        {checksDialog}
        <form onSubmit={form.handleSubmit(onSubmit)}>
          <CardContent className="mt-6">
            <div className="space-y-4">
              <FormField
                control={form.control}
                name="connectionId"
                render={({ field }) => (
                  <FormItem>
                    <FormControl>
                      <Select
                        onValueChange={(value: string) => {
                          field.onChange(value);
                          form.setValue(
                            `destinationOptions`,
                            {},
                            {
                              shouldDirty: true,
                              shouldTouch: true,
                              shouldValidate: true,
                            }
                          );
                        }}
                        value={field.value}
                      >
                        <SelectTrigger>
                          <SelectValue
                            ref={field.ref}
                            placeholder={dest?.name}
                          />
                        </SelectTrigger>
                        <SelectContent>
                          <ConnectionSelectContent
                            postgres={postgres}
                            mysql={mysql}
                            s3={s3}
                            mongodb={mongodb}
                            gcpcs={gcpcs}
                            dynamodb={dynamodb}
                            mssql={mssql}
                          />
                        </SelectContent>
                      </Select>
                    </FormControl>
                    <FormDescription>
                      The location of the destination data set.
                    </FormDescription>
                  </FormItem>
                )}
              />
              <DestinationAlerts
                connectionId={form.getValues('connectionId')}
                jobmappings={jobmappings}
                initTableSchemaEnabled={isInitTableSchemaEnabled(
                  form.getValues('destinationOptions')
                )}
              />
              <DestinationOptionsForm
                connection={connections.find(
                  (c) => c.id === form.getValues().connectionId
                )}
                value={destOpts}
                setValue={(newOpts) => {
                  form.setValue('destinationOptions', newOpts, {
                    shouldDirty: true,
                    shouldTouch: true,
                    shouldValidate: true,
                  });
                }}
                hideInitTableSchema={shouldHideInitTableSchema}
                hideDynamoDbTableMappings={true}
                destinationDetailsRecord={{}} // not used because we are hiding dynamodb table mappings
                errors={form.formState.errors?.destinationOptions}
              />
            </div>
          </CardContent>
          <CardFooter>
            <div className="flex flex-row items-center justify-between w-full mt-4">
              <DeleteButton isDisabled={isDeleteDisabled} onDelete={onDelete} />
              <Button disabled={!form.formState.isDirty} type="submit">
                Save
              </Button>
            </div>
          </CardFooter>
        </form>
      </Form>
    </Card>
  );
}

function isInitTableSchemaEnabled(
  destinationOptions: NewDestinationFormValues['destinationOptions']
): boolean {
  return (
    destinationOptions?.postgres?.initTableSchema ||
    destinationOptions?.mssql?.initTableSchema ||
    destinationOptions?.mysql?.initTableSchema ||
    false
  );
}

interface DeleteButtonProps {
  isDisabled?: boolean;
  onDelete(): Promise<void> | void;
}

function DeleteButton(props: DeleteButtonProps): ReactElement {
  const { isDisabled, onDelete } = props;
  return (
    <DeleteConfirmationDialog
      trigger={
        <Button type="button" variant="destructive" disabled={isDisabled}>
          <ButtonText leftIcon={<TrashIcon />} text="Delete" />
        </Button>
      }
      headerText="Are you sure you want to delete this destination connection?"
      description="Deleting this is irreversable and will cause data to stop syncing to this destination!"
      onConfirm={async () => onDelete()}
    />
  );
}

function useShouldHideInitConnectionSchema(
  control: Control<NewDestinationFormValues>,
  sourceId: string
): boolean {
  const [destinationConnectionid] = useWatch({
    control,
    name: ['connectionId'],
  });
  return destinationConnectionid === sourceId;
}
