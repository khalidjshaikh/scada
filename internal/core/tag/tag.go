package tag

import (
	"fmt"
	"sync"
	"time"
)

// Type is the data type of a tag.
type Type string

const (
	TypeBool   Type = "bool"
	TypeInt    Type = "int"
	TypeFloat  Type = "float"
	TypeString Type = "string"
)

// Quality describes data quality.
type Quality int

const (
	QualityGood Quality = iota
	QualityBad
	QualityUncertain
)

func (q Quality) String() string {
	switch q {
	case QualityGood:
		return "good"
	case QualityBad:
		return "bad"
	case QualityUncertain:
		return "uncertain"
	default:
		return "unknown"
	}
}

// Tag is a single data point in the SCADA system.
type Tag struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Type        Type              `json:"type"`
	Unit        string            `json:"unit"`
	Group       string            `json:"group"`
	Access      string            `json:"access"` // "ro" or "rw"
	Min         *float64          `json:"min,omitempty"`
	Max         *float64          `json:"max,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`

	mu    sync.RWMutex
	value any
	qual  Quality
	ts    time.Time
}

// New creates a tag with defaults.
func New(id, name string, t Type) *Tag {
	return &Tag{
		ID:     id,
		Name:   name,
		Type:   t,
		Access: "ro",
		qual:   QualityUncertain,
		ts:     time.Now(),
	}
}

func (t *Tag) SetValue(v any, q Quality) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.value = v
	t.qual = q
	t.ts = time.Now()
}

func (t *Tag) Value() any {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.value
}

func (t *Tag) Quality() Quality {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.qual
}

func (t *Tag) Timestamp() time.Time {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.ts
}

// Snapshot is a serializable copy of a tag's current state.
type Snapshot struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Type        Type              `json:"type"`
	Unit        string            `json:"unit"`
	Group       string            `json:"group"`
	Access      string            `json:"access"`
	Min         *float64          `json:"min,omitempty"`
	Max         *float64          `json:"max,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Value       any               `json:"value"`
	Quality     string            `json:"quality"`
	Timestamp   time.Time         `json:"timestamp"`
}

func (t *Tag) Snapshot() Snapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return Snapshot{
		ID:          t.ID,
		Name:        t.Name,
		Description: t.Description,
		Type:        t.Type,
		Unit:        t.Unit,
		Group:       t.Group,
		Access:      t.Access,
		Min:         t.Min,
		Max:         t.Max,
		Metadata:    t.Metadata,
		Value:       t.value,
		Quality:     t.qual.String(),
		Timestamp:   t.ts,
	}
}

// Coerce validates and converts a value to the tag's type.
func (t *Tag) Coerce(v any) (any, error) {
	switch t.Type {
	case TypeBool:
		switch x := v.(type) {
		case bool:
			return x, nil
		case float64:
			return x != 0, nil
		case int:
			return x != 0, nil
		default:
			return nil, errType("bool", v)
		}
	case TypeInt:
		switch x := v.(type) {
		case int:
			return x, nil
		case int64:
			return int(x), nil
		case float64:
			return int(x), nil
		case bool:
			if x {
				return 1, nil
			}
			return 0, nil
		default:
			return nil, errType("int", v)
		}
	case TypeFloat:
		switch x := v.(type) {
		case float64:
			return x, nil
		case int:
			return float64(x), nil
		case int64:
			return float64(x), nil
		case bool:
			if x {
				return 1.0, nil
			}
			return 0.0, nil
		default:
			return nil, errType("float", v)
		}
	case TypeString:
		return toString(v), nil
	}
	return v, nil
}

type typeErr struct{ msg string }

func (e typeErr) Error() string { return e.msg }

func errType(want string, v any) error {
	return typeErr{msg: "cannot convert value to " + want}
}

func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", x)
	}
}
