package husonymtypes

type Binary struct {
	BaseType    `       json:",inline"`
	JsonScanner `       json:"-"`
	Bytes       []byte `json:"bytes"`
}

func (b *Binary) ScanPgx(value any) error {
	if value == nil {
		return nil
	}
	b.Bytes = value.([]byte)
	return nil
}

func (b *Binary) ValuePgx() (any, error) {
	if b == nil || b.Bytes == nil {
		return nil, nil
	}
	return b.Bytes, nil
}

func (b *Binary) ScanMysql(value any) error {
	if value == nil {
		return nil
	}
	b.Bytes = value.([]byte)
	return nil
}

func (b *Binary) ValueMysql() (any, error) {
	if b == nil || b.Bytes == nil {
		return nil, nil
	}
	return b.Bytes, nil
}

func (b *Binary) ScanMssql(value any) error {
	return b.ScanMysql(value)
}

func (b *Binary) ValueMssql() (any, error) {
	if b == nil || b.Bytes == nil {
		return nil, nil
	}
	return b.Bytes, nil
}

func (b *Binary) ScanJson(value any) error {
	return b.JsonScanner.ScanJson(value, b)
}

func (b *Binary) ValueJson() (any, error) {
	return b.JsonScanner.ValueJson(b)
}

func (b *Binary) setVersion(v Version) {
	b.Husonym.Version = v
}

func (b *Binary) GetVersion() Version {
	return b.Husonym.Version
}

func NewBinary(opts ...HusonymTypeOption) (*Binary, error) {
	binary := &Binary{}
	binary.Husonym.TypeId = HusonymBinaryId
	binary.setVersion(LatestVersion)

	if err := applyOptions(binary, opts...); err != nil {
		return nil, err
	}
	return binary, nil
}

func NewBinaryFromPgx(value any, opts ...HusonymTypeOption) (*Binary, error) {
	binary, err := NewBinary(opts...)
	if err != nil {
		return nil, err
	}
	err = binary.ScanPgx(value)
	if err != nil {
		return nil, err
	}
	return binary, nil
}

func NewBinaryFromMysql(value any, opts ...HusonymTypeOption) (*Binary, error) {
	binary, err := NewBinary(opts...)
	if err != nil {
		return nil, err
	}
	err = binary.ScanMysql(value)
	if err != nil {
		return nil, err
	}
	return binary, nil
}

func NewBinaryFromMssql(value any, opts ...HusonymTypeOption) (*Binary, error) {
	binary, err := NewBinary(opts...)
	if err != nil {
		return nil, err
	}
	err = binary.ScanMssql(value)
	if err != nil {
		return nil, err
	}
	return binary, nil
}

func NewBinaryArrayFromPgx(
	elements [][]byte,
	opts []HusonymTypeOption,
	arrayOpts ...HusonymTypeOption,
) (*HusonymArray, error) {
	husonymAdapters := make([]HusonymAdapter, len(elements))
	for i, e := range elements {
		newBinary, err := NewBinary(opts...)
		if err != nil {
			return nil, err
		}
		husonymAdapters[i] = newBinary
		err = husonymAdapters[i].ScanPgx(e)
		if err != nil {
			return nil, err
		}
	}
	return NewHusonymArray(husonymAdapters)
}
