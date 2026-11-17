package driver

import (
	"context"
	"testing"
	"time"

	"github.com/kshaikh/scada/internal/core/db"
	"github.com/kshaikh/scada/internal/core/tag"
	"github.com/kshaikh/scada/internal/protocol/modbus"
)

func TestModbusDriverPoll(t *testing.T) {
	bank := modbus.NewBank(100)
	srv := modbus.NewServer("127.0.0.1:1503", bank)
	go srv.ListenAndServe()
	defer srv.Shutdown()
	time.Sleep(100 * time.Millisecond)

	bank.Holding[0] = 42
	bank.Coils[0] = true

	maps := []RegisterMap{
		{TagID: "t1", Type: "holding", Address: 0, DataType: "uint16", Scale: 1, Writable: true},
		{TagID: "t2", Type: "coil", Address: 0, DataType: "bool"},
	}
	md := NewModbus("mb", "127.0.0.1", 1503, 1, 100*time.Millisecond, maps)

	database := db.New(nil)
	database.Add(tag.New("t1", "T1", tag.TypeFloat))
	database.Add(tag.New("t2", "T2", tag.TypeBool))

	md.Connect(context.Background())
	md.Poll(context.Background(), database)

	v, _ := database.Get("t1")
	if v.Value() != float64(42) {
		t.Fatalf("got %v", v.Value())
	}
	v2, _ := database.Get("t2")
	if v2.Value() != true {
		t.Fatalf("got %v", v2.Value())
	}
}
