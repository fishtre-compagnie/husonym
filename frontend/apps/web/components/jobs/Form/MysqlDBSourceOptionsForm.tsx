import { MysqlSourceOptionsFormValues } from '@/yup-validations/jobs';
import { ReactElement } from 'react';
import SchemaChangeStrategiesForm from './SchemaChangeStrategiesForm';

interface Props {
  value: MysqlSourceOptionsFormValues;
  setValue(newVal: MysqlSourceOptionsFormValues): void;
}

export default function MysqlDBSourceOptionsForm(props: Props): ReactElement {
  const { value, setValue } = props;
  return (
    <SchemaChangeStrategiesForm
      newColumn={value.newColumnAdditionStrategy}
      removedColumn={value.columnRemovalStrategy}
      onNewColumnChange={(strategy) =>
        setValue({ ...value, newColumnAdditionStrategy: strategy })
      }
      onRemovedColumnChange={(strategy) =>
        setValue({ ...value, columnRemovalStrategy: strategy })
      }
    />
  );
}
