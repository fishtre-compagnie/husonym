package husonym_benthos_sql

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/fishtre-compagnie/husonym/internal/gotypeutil"
	husonymtypes "github.com/fishtre-compagnie/husonym/internal/husonym-types"
	husonym_benthos "github.com/fishtre-compagnie/husonym/worker/pkg/benthos"
	"github.com/redpanda-data/benthos/v4/public/service"
)

func husonymToMssqlProcessorConfig() *service.ConfigSpec {
	return service.NewConfigSpec().
		Field(service.NewStringListField("columns")).
		Field(service.NewStringMapField("column_data_types")).
		Field(service.NewAnyMapField("column_default_properties"))
}

func RegisterHusonymToMssqlProcessor(env *service.Environment) error {
	return env.RegisterBatchProcessor(
		"husonym_to_mssql",
		husonymToMssqlProcessorConfig(),
		func(conf *service.ParsedConfig, mgr *service.Resources) (service.BatchProcessor, error) {
			proc, err := newHusonymToMssqlProcessor(conf, mgr)
			if err != nil {
				return nil, err
			}
			return proc, nil
		})
}

type husonymToMssqlProcessor struct {
	logger                  *service.Logger
	columns                 []string
	columnDataTypes         map[string]string
	columnDefaultProperties map[string]*husonym_benthos.ColumnDefaultProperties
}

func newHusonymToMssqlProcessor(
	conf *service.ParsedConfig,
	mgr *service.Resources,
) (*husonymToMssqlProcessor, error) {
	columns, err := conf.FieldStringList("columns")
	if err != nil {
		return nil, err
	}

	columnDataTypes, err := conf.FieldStringMap("column_data_types")
	if err != nil {
		return nil, err
	}

	columnDefaultPropertiesConfig, err := conf.FieldAnyMap("column_default_properties")
	if err != nil {
		return nil, err
	}

	columnDefaultProperties, err := getColumnDefaultProperties(columnDefaultPropertiesConfig)
	if err != nil {
		return nil, err
	}

	return &husonymToMssqlProcessor{
		logger:                  mgr.Logger(),
		columns:                 columns,
		columnDataTypes:         columnDataTypes,
		columnDefaultProperties: columnDefaultProperties,
	}, nil
}

func (p *husonymToMssqlProcessor) ProcessBatch(
	ctx context.Context,
	batch service.MessageBatch,
) ([]service.MessageBatch, error) {
	newBatch := make(service.MessageBatch, 0, len(batch))
	for _, msg := range batch {
		root, err := msg.AsStructuredMut()
		if err != nil {
			return nil, err
		}
		newRoot, err := transformHusonymToMssql(root, p.columns, p.columnDefaultProperties)
		if err != nil {
			return nil, err
		}
		newMsg := msg.Copy()
		newMsg.SetStructured(newRoot)
		newBatch = append(newBatch, newMsg)
	}

	if len(newBatch) == 0 {
		return nil, nil
	}
	return []service.MessageBatch{newBatch}, nil
}

func (m *husonymToMssqlProcessor) Close(context.Context) error {
	return nil
}

func transformHusonymToMssql(
	root any,
	columns []string,
	columnDefaultProperties map[string]*husonym_benthos.ColumnDefaultProperties,
) (map[string]any, error) {
	rootMap, ok := root.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("root value must be a map[string]any")
	}

	newMap := make(map[string]any)
	for col, val := range rootMap {
		// Skip values that aren't in the column list to handle circular references
		if !isColumnInList(col, columns) {
			continue
		}

		colDefaults := columnDefaultProperties[col]
		// sqlserver doesn't support default values. must be removed
		if colDefaults != nil && colDefaults.HasDefaultTransformer {
			continue
		}

		newVal, err := getMssqlValue(val)
		if err != nil {
			return nil, fmt.Errorf("failed to get MSSQL value for column %s: %w", col, err)
		}
		newMap[col] = newVal
	}

	return newMap, nil
}

func getMssqlValue(value any) (any, error) {
	value, isHusonymValue, err := getMssqlHusonymValue(value)
	if err != nil {
		return nil, err
	}
	if isHusonymValue {
		return value, nil
	}
	if gotypeutil.IsMap(value) {
		bits, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("unable to marshal go map to json bits: %w", err)
		}
		return bits, nil
	}

	return value, nil
}

func getMssqlHusonymValue(root any) (value any, isHusonymValue bool, err error) {
	if valuer, ok := root.(husonymtypes.HusonymMssqlValuer); ok {
		value, err := valuer.ValueMssql()
		if err != nil {
			return nil, false, fmt.Errorf(
				"unable to get MSSQL value from HusonymMssqlValuer: %w",
				err,
			)
		}
		return value, true, nil
	}
	return root, false, nil
}
