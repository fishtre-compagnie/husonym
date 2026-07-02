package husonym_benthos_sql

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/fishtre-compagnie/husonym/internal/gotypeutil"
	husonymtypes "github.com/fishtre-compagnie/husonym/internal/husonym-types"
	husonym_benthos "github.com/fishtre-compagnie/husonym/worker/pkg/benthos"
	"github.com/doug-martin/goqu/v9"
	"github.com/redpanda-data/benthos/v4/public/service"
)

func husonymToMysqlProcessorConfig() *service.ConfigSpec {
	return service.NewConfigSpec().
		Field(service.NewStringListField("columns")).
		Field(service.NewStringMapField("column_data_types")).
		Field(service.NewAnyMapField("column_default_properties"))
}

func RegisterHusonymToMysqlProcessor(env *service.Environment) error {
	return env.RegisterBatchProcessor(
		"husonym_to_mysql",
		husonymToMysqlProcessorConfig(),
		func(conf *service.ParsedConfig, mgr *service.Resources) (service.BatchProcessor, error) {
			proc, err := newHusonymToMysqlProcessor(conf, mgr)
			if err != nil {
				return nil, err
			}
			return proc, nil
		})
}

type husonymToMysqlProcessor struct {
	logger                  *service.Logger
	columns                 []string
	columnDataTypes         map[string]string
	columnDefaultProperties map[string]*husonym_benthos.ColumnDefaultProperties
}

func newHusonymToMysqlProcessor(
	conf *service.ParsedConfig,
	mgr *service.Resources,
) (*husonymToMysqlProcessor, error) {
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

	return &husonymToMysqlProcessor{
		logger:                  mgr.Logger(),
		columns:                 columns,
		columnDataTypes:         columnDataTypes,
		columnDefaultProperties: columnDefaultProperties,
	}, nil
}

func (p *husonymToMysqlProcessor) ProcessBatch(
	ctx context.Context,
	batch service.MessageBatch,
) ([]service.MessageBatch, error) {
	newBatch := make(service.MessageBatch, 0, len(batch))
	for _, msg := range batch {
		root, err := msg.AsStructuredMut()
		if err != nil {
			return nil, err
		}
		newRoot, err := transformHusonymToMysql(
			root,
			p.columns,
			p.columnDataTypes,
			p.columnDefaultProperties,
		)
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

func (m *husonymToMysqlProcessor) Close(context.Context) error {
	return nil
}

func transformHusonymToMysql(
	root any,
	columns []string,
	columnDataTypes map[string]string,
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
		datatype := columnDataTypes[col]
		newVal, err := getMysqlValue(val, colDefaults, datatype)
		if err != nil {
			return nil, fmt.Errorf("failed to get MySQL value for column %s: %w", col, err)
		}
		newMap[col] = newVal
	}

	return newMap, nil
}

func getMysqlValue(
	value any,
	colDefaults *husonym_benthos.ColumnDefaultProperties,
	datatype string,
) (any, error) {
	if colDefaults != nil && colDefaults.HasDefaultTransformer {
		return goqu.Default(), nil
	}

	if value == nil {
		return nil, nil
	}

	value, isHusonymValue, err := getMysqlHusonymValue(value)
	if err != nil {
		return nil, fmt.Errorf("unable to get MySQL value from husonym value: %w", err)
	}
	if isHusonymValue {
		return value, nil
	}

	if datatype == "json" {
		if v, ok := value.([]byte); ok {
			validJson, err := getValidJson(v)
			if err != nil {
				return nil, fmt.Errorf("unable to get valid json: %w", err)
			}
			return validJson, nil
		}
		if value == "null" {
			return value, nil
		}
		bits, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("unable to marshal mysql json to bits: %w", err)
		}
		return bits, nil
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

func getMysqlHusonymValue(root any) (value any, isHusonymValue bool, err error) {
	if valuer, ok := root.(husonymtypes.HusonymMysqlValuer); ok {
		value, err := valuer.ValueMysql()
		if err != nil {
			return nil, false, fmt.Errorf(
				"unable to get MYSQL value from HusonymMysqlValuer: %w",
				err,
			)
		}
		return value, true, nil
	}
	return root, false, nil
}
