package husonymtypes

import (
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

type HusonymDateTime struct {
	BaseType    `json:",inline"`
	JsonScanner `json:"-"`

	Year   int `json:"year"`   // Absolute year number (converted to negative for BC dates)
	Month  int `json:"month"`  // 1-12
	Day    int `json:"day"`    // 1-31
	Hour   int `json:"hour"`   // 0-23
	Minute int `json:"minute"` // 0-59
	Second int `json:"second"` // 0-59
	Nano   int `json:"nano"`   // 0-999999999

	TimeZone  string `json:"timezone,omitempty"`
	Precision int8   `json:"precision,omitempty"`
	IsBC      bool   `json:"is_bc,omitempty"`
}

func (dt *HusonymDateTime) setVersion(v Version) {
	dt.Husonym.Version = v
}

func (dt *HusonymDateTime) GetVersion() Version {
	return dt.Husonym.Version
}

func (dt *HusonymDateTime) ScanJson(value any) error {
	return dt.JsonScanner.ScanJson(value, dt)
}

func (dt *HusonymDateTime) ValueJson() (any, error) {
	return dt.JsonScanner.ValueJson(dt)
}

func (dt *HusonymDateTime) ScanPgx(value any) error {
	if value == nil {
		return nil
	}

	var t time.Time
	var valid bool
	var tz string

	switch v := value.(type) {
	case *pgtype.Timestamp:
		if !v.Valid {
			return nil
		}
		t = v.Time
		valid = true
	case *pgtype.Timestamptz:
		if !v.Valid {
			return nil
		}
		t = v.Time
		valid = true
		tz = t.Location().String()
	case *pgtype.Date:
		if !v.Valid {
			return nil
		}
		t = v.Time
		valid = true
		tz = "UTC"
	case time.Time:
		t = v
		valid = true
		if loc := t.Location(); loc != time.UTC {
			tz = loc.String()
		}
	default:
		return fmt.Errorf("unsupported type for DateTime: %T", value)
	}

	if !valid {
		return nil
	}

	dt.IsBC = t.Year() <= 0
	year := t.Year()
	if dt.IsBC {
		year = -year + 1 // Convert BC year to positive internal format
	}
	dt.Year = year
	dt.Month = int(t.Month())
	dt.Day = t.Day()
	dt.Hour = t.Hour()
	dt.Minute = t.Minute()
	dt.Second = t.Second()
	dt.Nano = t.Nanosecond()
	dt.TimeZone = tz

	return nil
}

func (dt *HusonymDateTime) ValuePgx() (any, error) {
	year := dt.Year
	if dt.IsBC {
		return dt.handlePgxBCDate()
	}

	if dt.TimeZone != "" {
		loc, err := time.LoadLocation(dt.TimeZone)
		if err != nil {
			return nil, fmt.Errorf("invalid timezone: %w", err)
		}
		// Create the time directly in the target timezone
		t := time.Date(
			year,
			time.Month(dt.Month),
			dt.Day,
			dt.Hour,
			dt.Minute,
			dt.Second,
			dt.Nano,
			loc,
		)
		return &pgtype.Timestamptz{Time: t, Valid: true}, nil
	}

	// For non-timezone times, create in UTC
	t := time.Date(
		year,
		time.Month(dt.Month),
		dt.Day,
		dt.Hour,
		dt.Minute,
		dt.Second,
		dt.Nano,
		time.UTC,
	)

	// If precision is -1, treat as date
	if dt.Precision == -1 {
		return &pgtype.Date{Time: t, Valid: true}, nil
	}

	return &pgtype.Timestamp{Time: t, Valid: true}, nil
}

func (dt *HusonymDateTime) handlePgxBCDate() (any, error) {
	year := -dt.Year + 1 // Convert to BC year

	t := time.Date(
		year,
		time.Month(dt.Month),
		dt.Day,
		dt.Hour,
		dt.Minute,
		dt.Second,
		dt.Nano,
		time.UTC,
	)

	if dt.TimeZone != "" {
		return &pgtype.Timestamptz{Time: t, Valid: true}, nil
	}

	if dt.Precision == -1 {
		return &pgtype.Date{Time: t, Valid: true}, nil
	}

	return &pgtype.Timestamp{Time: t, Valid: true}, nil
}

func (dt *HusonymDateTime) ScanMysql(value any) error {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case time.Time:
		dt.IsBC = v.Year() <= 0
		year := v.Year()
		if dt.IsBC {
			year = -year + 1 // Convert BC year to positive internal format
		}
		dt.Year = year
		dt.Month = int(v.Month())
		dt.Day = v.Day()
		dt.Hour = v.Hour()
		dt.Minute = v.Minute()
		dt.Second = v.Second()
		dt.Nano = v.Nanosecond()
		if loc := v.Location(); loc != time.UTC {
			dt.TimeZone = loc.String()
		}
		return nil
	default:
		return fmt.Errorf("unsupported type for DateTime: %T", value)
	}
}

func (dt *HusonymDateTime) ValueMysql() (any, error) {
	year := dt.Year
	if dt.IsBC {
		year = -year + 1 // Convert to BC year
	}

	loc := time.UTC
	if dt.TimeZone != "" {
		var err error
		loc, err = time.LoadLocation(dt.TimeZone)
		if err != nil {
			return nil, fmt.Errorf("invalid timezone: %w", err)
		}
	}

	t := time.Date(
		year,
		time.Month(dt.Month),
		dt.Day,
		dt.Hour,
		dt.Minute,
		dt.Second,
		dt.Nano,
		loc,
	)

	// For dates with only year/month/day, return as DATE
	if dt.Hour == 0 && dt.Minute == 0 && dt.Second == 0 && dt.Nano == 0 {
		return t.Format(time.DateOnly), nil
	}

	// Otherwise return as DATETIME
	return t.Format(time.DateTime), nil
}

func (dt *HusonymDateTime) ScanMssql(value any) error {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case time.Time:
		dt.Year = v.Year()
		dt.Month = int(v.Month())
		dt.Day = v.Day()
		dt.Hour = v.Hour()
		dt.Minute = v.Minute()
		dt.Second = v.Second()
		dt.Nano = v.Nanosecond()
		dt.TimeZone = v.Format("-07:00")
		return nil
	default:
		return fmt.Errorf("unsupported type for DateTime: %T", value)
	}
}

func (dt *HusonymDateTime) ValueMssql() (any, error) {
	year := dt.Year

	loc := time.UTC
	if dt.TimeZone != "" {
		// Handle timezone offset format like "+02:00"
		offset, err := time.Parse("-07:00", dt.TimeZone)
		if err == nil {
			// Convert the parsed time's UTC offset into seconds
			_, offsetSeconds := offset.Zone()
			loc = time.FixedZone("", offsetSeconds)
		} else {
			// Try loading as named timezone if not an offset
			var tzErr error
			loc, tzErr = time.LoadLocation(dt.TimeZone)
			if tzErr != nil {
				return nil, fmt.Errorf("invalid timezone: %w", tzErr)
			}
		}
	}

	t := time.Date(
		year,
		time.Month(dt.Month),
		dt.Day,
		dt.Hour,
		dt.Minute,
		dt.Second,
		dt.Nano,
		loc,
	)

	// For dates with only year/month/day, return as DATE
	if dt.Hour == 0 && dt.Minute == 0 && dt.Second == 0 && dt.Nano == 0 {
		return t.Format(time.DateOnly), nil
	}

	return t, nil
}

func NewDateTimeFromMysql(value any, opts ...HusonymTypeOption) (*HusonymDateTime, error) {
	dt, err := NewDateTime(opts...)
	if err != nil {
		return nil, err
	}
	if err := dt.ScanMysql(value); err != nil {
		return nil, err
	}
	return dt, nil
}

func NewDateTimeFromMssql(value any, opts ...HusonymTypeOption) (*HusonymDateTime, error) {
	dt, err := NewDateTime(opts...)
	if err != nil {
		return nil, err
	}
	if err := dt.ScanMssql(value); err != nil {
		return nil, err
	}
	return dt, nil
}

func NewDateTimeFromPgx(value any, opts ...HusonymTypeOption) (*HusonymDateTime, error) {
	dt, err := NewDateTime(opts...)
	if err != nil {
		return nil, err
	}
	if err := dt.ScanPgx(value); err != nil {
		return nil, err
	}
	return dt, nil
}

func NewDateTime(opts ...HusonymTypeOption) (*HusonymDateTime, error) {
	dt := &HusonymDateTime{}
	dt.Husonym.TypeId = HusonymDateTimeId
	dt.setVersion(LatestVersion)
	if err := applyOptions(dt, opts...); err != nil {
		return nil, err
	}
	return dt, nil
}

func NewDateTimeArrayFromPgx(
	elements []time.Time,
	opts []HusonymTypeOption,
	arrayOpts ...HusonymTypeOption,
) (*HusonymArray, error) {
	husonymAdapters := make([]HusonymAdapter, len(elements))
	for i, e := range elements {
		newDateTime, err := NewDateTime(opts...)
		if err != nil {
			return nil, err
		}
		husonymAdapters[i] = newDateTime
		err = husonymAdapters[i].ScanPgx(e)
		if err != nil {
			return nil, err
		}
	}
	return NewHusonymArray(husonymAdapters)
}
