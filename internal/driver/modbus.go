package driver

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"time"

	"github.com/kshaikh/scada/internal/core/db"
	"github.com/kshaikh/scada/internal/core/tag"
	"github.com/kshaikh/scada/internal/protocol/modbus"
)

type RegisterMap struct {
	TagID     string  `yaml:"tag_id" json:"tag_id"`
	Type      string  `yaml:"type" json:"type"` // holding, input, coil, discrete
	Address   uint16  `yaml:"address" json:"address"`
	Quantity  uint16  `yaml:"quantity" json:"quantity"`
	DataType  string  `yaml:"data_type" json:"data_type"` // uint16, int16, float32, bool
	Scale     float64 `yaml:"scale" json:"scale"`
	Offset    float64 `yaml:"offset" json:"offset"`
	Writable  bool    `yaml:"writable" json:"writable"`
}

type ModbusDriver struct {
	name     string
	addr     string
	unit     byte
	interval time.Duration
	client   *modbus.Client
	maps     []RegisterMap
}

func NewModbus(name, host string, port int, unit byte, interval time.Duration, maps []RegisterMap) *ModbusDriver {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	if unit == 0 {
		unit = 1
	}
	addr := fmt.Sprintf("%s:%d", host, port)
	return &ModbusDriver{
		name: name, addr: addr, unit: unit, interval: interval,
		client: modbus.NewClient(addr, 5*time.Second, unit),
		maps:   maps,
	}
}

func (m *ModbusDriver) Name() string { return m.name }
func (m *ModbusDriver) Interval() time.Duration { return m.interval }
func (m *ModbusDriver) Connect(ctx context.Context) error {
	return m.client.Connect()
}
func (m *ModbusDriver) Disconnect() error {
	m.client.Close()
	return nil
}

func (m *ModbusDriver) Poll(ctx context.Context, database *db.DB) error {
	for _, rm := range m.maps {
		switch rm.Type {
		case "holding", "input":
			qty := rm.Quantity
			if qty == 0 {
				if rm.DataType == "float32" {
					qty = 2
				} else {
					qty = 1
				}
			}
			var regs []uint16
			var err error
			if rm.Type == "holding" {
				regs, err = m.client.ReadHoldingRegisters(rm.Address, qty)
			} else {
				regs, err = m.client.ReadInputRegisters(rm.Address, qty)
			}
			if err != nil {
				continue
			}
			val := decodeRegs(regs, rm)
			if tg, ok := database.Get(rm.TagID); ok {
				database.Set(rm.TagID, val, tag.QualityGood)
				_ = tg
			}
		case "coil", "discrete":
			bs, err := m.client.ReadCoils(rm.Address, 1)
			_ = bs
			if err != nil {
				// try discrete? client only coils; server stores separate but read coils works for coils
			}
			if err == nil && len(bs) > 0 {
				database.Set(rm.TagID, bs[0], tag.QualityGood)
			}
		}
	}
	return nil
}

func (m *ModbusDriver) Write(ctx context.Context, database *db.DB, tagID string, value any) error {
	for _, rm := range m.maps {
		if rm.TagID != tagID || !rm.Writable {
			continue
		}
		switch rm.Type {
		case "holding":
			switch rm.DataType {
			case "float32":
				f, _ := value.(float64)
				bits := mathFloat32(f)
				_ = m.client.WriteMultipleRegisters(rm.Address, []uint16{uint16(bits >> 16), uint16(bits)})
			case "uint16", "int16":
				v, _ := value.(int)
				m.client.WriteSingleRegister(rm.Address, uint16(v))
			}
		case "coil":
			b, _ := value.(bool)
			m.client.WriteSingleCoil(rm.Address, b)
		}
		_ = database.Set(tagID, value, tag.QualityGood)
		return nil
	}
	return fmt.Errorf("not found")
}

func decodeRegs(regs []uint16, rm RegisterMap) any {
	switch rm.DataType {
	case "float32":
		if len(regs) < 2 {
			return 0.0
		}
		bits := uint32(regs[0])<<16 | uint32(regs[1])
		f := float32frombits(bits)
		return float64(f)*rm.Scale + rm.Offset
	case "int16":
		if len(regs) < 1 {
			return 0
		}
		v := int16(regs[0])
		return (float64(v)*rm.Scale + rm.Offset)
	case "uint16":
		if len(regs) < 1 {
			return 0
		}
		v := float64(regs[0])
		return v*rm.Scale + rm.Offset
	default:
		if len(regs) > 0 {
			return regs[0]
		}
		return 0
	}
}

func mathFloat32(f float64) uint32 {
	return math.Float32bits(float32(f))
}

func float32frombits(bits uint32) float32 {
	return math.Float32frombits(bits)
}

// decodeRegs float32 case uses float32frombits.
var _ = binary.BigEndian
