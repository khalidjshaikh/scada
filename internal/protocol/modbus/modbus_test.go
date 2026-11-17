package modbus

import (
	"testing"
	"time"
)

func TestRoundTrip(t *testing.T) {
	srv := NewServer("127.0.0.1:1502", NewBank(100))
	go srv.ListenAndServe()
	defer srv.Shutdown()
	time.Sleep(100 * time.Millisecond)

	c := NewClient("127.0.0.1:1502", 2*time.Second, 1)
	if err := c.Connect(); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	srv.Bank().Holding[5] = 1234
	regs, err := c.ReadHoldingRegisters(5, 1)
	if err != nil || len(regs) != 1 || regs[0] != 1234 {
		t.Fatalf("read holding: %v %v", regs, err)
	}

	if err := c.WriteSingleRegister(10, 999); err != nil {
		t.Fatal(err)
	}
	srv.Bank().mu.RLock()
	v := srv.Bank().Holding[10]
	srv.Bank().mu.RUnlock()
	if v != 999 {
		t.Fatalf("write single: %d", v)
	}

	if err := c.WriteMultipleRegisters(20, []uint16{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	srv.Bank().mu.RLock()
	if srv.Bank().Holding[20] != 1 || srv.Bank().Holding[22] != 3 {
		t.Fatalf("write multi")
	}
	srv.Bank().mu.RUnlock()

	srv.Bank().Coils[7] = true
	bs, err := c.ReadCoils(7, 1)
	if err != nil || len(bs) != 1 || !bs[0] {
		t.Fatalf("read coils: %v %v", bs, err)
	}
	if err := c.WriteSingleCoil(8, true); err != nil {
		t.Fatal(err)
	}
	srv.Bank().mu.RLock()
	if !srv.Bank().Coils[8] {
		t.Fatal("coil not set")
	}
	srv.Bank().mu.RUnlock()
}
