package husonymtypes

import (
	"fmt"

	"github.com/fishtre-compagnie/husonym/internal/gotypeutil"
	"github.com/lib/pq"
)

type HusonymArray struct {
	BaseType `                 json:",inline"`
	Elements []HusonymAdapter `json:"elements"`
}

func NewHusonymArray(
	elements []HusonymAdapter,
	opts ...HusonymTypeOption,
) (*HusonymArray, error) {
	pgArray := &HusonymArray{
		Elements: elements,
	}
	pgArray.Husonym.TypeId = HusonymArrayId
	pgArray.setVersion(LatestVersion)

	if err := applyOptions(pgArray, opts...); err != nil {
		return nil, err
	}

	return pgArray, nil
}

func (a *HusonymArray) setVersion(v Version) {
	a.Husonym.Version = v
}

func (a *HusonymArray) GetVersion() Version {
	return a.Husonym.Version
}

func (a *HusonymArray) ScanPgx(value any) error {
	valueSlice, err := gotypeutil.ParseSlice(value)
	if err != nil {
		return err
	}
	if len(valueSlice) != len(a.Elements) {
		return fmt.Errorf(
			"length mismatch: got %d elements, expected %d",
			len(valueSlice),
			len(a.Elements),
		)
	}
	for i, v := range valueSlice {
		if err := a.Elements[i].ScanPgx(v); err != nil {
			return fmt.Errorf("scanning element %d: %w", i, err)
		}
	}
	return nil
}

func (a *HusonymArray) ValuePgx() (any, error) {
	values := make([]any, len(a.Elements))
	for i, e := range a.Elements {
		v, err := e.ValuePgx()
		if err != nil {
			return nil, fmt.Errorf("getting value for element %d: %w", i, err)
		}
		values[i] = v
	}
	return pq.Array(values), nil
}

func (a *HusonymArray) ScanJson(value any) error {
	valueSlice, err := gotypeutil.ParseSlice(value)
	if err != nil {
		return err
	}
	if len(valueSlice) != len(a.Elements) {
		return fmt.Errorf(
			"length mismatch: got %d elements, expected %d",
			len(valueSlice),
			len(a.Elements),
		)
	}
	for i, v := range valueSlice {
		if err := a.Elements[i].ScanJson(v); err != nil {
			return fmt.Errorf("scanning element %d: %w", i, err)
		}
	}
	return nil
}

func (a *HusonymArray) ValueJson() (any, error) {
	values := make([]any, len(a.Elements))
	for i, e := range a.Elements {
		v, err := e.ValueJson()
		if err != nil {
			return nil, fmt.Errorf("getting value for element %d: %w", i, err)
		}
		values[i] = v
	}
	return values, nil
}

func (a *HusonymArray) ScanMysql(value any) error {
	valueSlice, err := gotypeutil.ParseSlice(value)
	if err != nil {
		return err
	}
	if len(valueSlice) != len(a.Elements) {
		return fmt.Errorf(
			"length mismatch: got %d elements, expected %d",
			len(valueSlice),
			len(a.Elements),
		)
	}
	for i, v := range valueSlice {
		if err := a.Elements[i].ScanMysql(v); err != nil {
			return fmt.Errorf("scanning element %d: %w", i, err)
		}
	}
	return nil
}

func (a *HusonymArray) ValueMysql() (any, error) {
	values := make([]any, len(a.Elements))
	for i, e := range a.Elements {
		v, err := e.ValueMysql()
		if err != nil {
			return nil, fmt.Errorf("getting value for element %d: %w", i, err)
		}
		values[i] = v
	}
	return values, nil
}

func (a *HusonymArray) ScanMssql(value any) error {
	return a.ScanMysql(value)
}

func (a *HusonymArray) ValueMssql() (any, error) {
	return a.ValueMysql()
}
