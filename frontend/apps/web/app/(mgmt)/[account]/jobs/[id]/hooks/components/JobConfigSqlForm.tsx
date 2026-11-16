import { ReactElement } from 'react';

import ConnectionSelectContent from '@/app/(mgmt)/[account]/new/job/connect/ConnectionSelectContent';
import FormErrorMessage from '@/components/FormErrorMessage';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import useMonacoResizer from '@/libs/hooks/monaco/useMonacoResizer';
import useMonacoTheme from '@/libs/hooks/monaco/useMonacoTheme';
import { splitConnections } from '@/libs/utils';
import { Editor } from '@monaco-editor/react';
import { Connection } from '@husonym/sdk';
import { editor } from 'monaco-editor';
import FormHeader from './FormHeader';
import { JobHookSqlFormValues, SqlTimingFormValue } from './validation';
import { useTranslations } from 'next-intl';

interface Props {
  values: JobHookSqlFormValues;
  setValues(values: JobHookSqlFormValues): void;
  jobConnections: Connection[];
  errors: Record<string, string>;
}

export default function JobConfigSqlForm(props: Props): ReactElement {
  const { values, setValues, jobConnections, errors } = props;
  const t = useTranslations('jobs.hooks.form');
  return (
    <>
      <SelectConnections
        jobConnections={jobConnections}
        selectedConnectionId={values.connectionId}
        setSelectedConnectionId={(updatedId) => {
          setValues({ ...values, connectionId: updatedId });
        }}
        error={errors['config.sql.connectionId']}
      />
      <div className="flex flex-col gap-3">
        <FormHeader
          title={t('query')}
          description={t('queryDescription')}
          isErrored={!!errors['config.sql.query']}
        />
        <EditSqlQuery
          query={values.query}
          setQuery={(query) => setValues({ ...values, query })}
        />
        <FormErrorMessage message={errors['config.sql.query']} />
      </div>
      <div className="flex flex-col gap-3">
        <FormHeader
          title={t('timing')}
          description={t('timingDescription')}
          htmlFor="timing"
          isErrored={!!errors['config.sql.timing']}
        />
        <Select
          name="timing"
          value={values.timing}
          onValueChange={(newValue) => {
            if (newValue) {
              setValues({ ...values, timing: newValue as SqlTimingFormValue });
            }
          }}
        >
          <SelectTrigger>
            <SelectValue placeholder={t('timingPlaceholder')} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="preSync">{t('preSync')}</SelectItem>
            <SelectItem value="postSync">{t('postSync')}</SelectItem>
          </SelectContent>
        </Select>
        <FormErrorMessage message={errors['config.sql.timing']} />
      </div>
    </>
  );
}

interface EditSqlQueryProps {
  query: string;
  setQuery(query: string): void;
}

const editorOptions: editor.IStandaloneEditorConstructionOptions = {
  minimap: { enabled: false },
  lineNumbers: 'off',
};

function EditSqlQuery(props: EditSqlQueryProps): ReactElement {
  const { query, setQuery } = props;

  const theme = useMonacoTheme();
  const { ref, width: editorWidth } = useMonacoResizer();

  return (
    <div
      className="monaco-editor-container rounded-lg border border-border p-2"
      ref={ref}
    >
      <Editor
        height="10vh"
        width={editorWidth}
        language="sql"
        theme={theme}
        options={editorOptions}
        value={query}
        onChange={(value) => {
          if (value) {
            setQuery(value);
          }
        }}
      />
    </div>
  );
}

interface SelectConnectionsProps {
  jobConnections: Connection[];

  selectedConnectionId: string;
  setSelectedConnectionId(id: string): void;
  error?: string;
}
function SelectConnections(props: SelectConnectionsProps): ReactElement {
  const {
    jobConnections,
    selectedConnectionId,
    setSelectedConnectionId,
    error,
  } = props;
  const t = useTranslations('jobs.hooks.form');
  const { postgres, mysql, mssql } = splitConnections(jobConnections);
  return (
    <div className="flex flex-col gap-3">
      <FormHeader
        htmlFor="connectionId"
        title={t('connection')}
        description={t('connectionDescription')}
        isErrored={!!error}
      />
      <Select
        name="connectionId"
        value={selectedConnectionId}
        onValueChange={(newValue) => {
          if (newValue) {
            setSelectedConnectionId(newValue);
          }
        }}
      >
        <SelectTrigger>
          <SelectValue placeholder={t('connectionPlaceholder')} />
        </SelectTrigger>
        <SelectContent>
          <ConnectionSelectContent
            postgres={postgres}
            mysql={mysql}
            mssql={mssql}
          />
        </SelectContent>
      </Select>
      <FormErrorMessage message={error} />
    </div>
  );
}
