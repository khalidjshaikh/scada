package modbus

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

const (
	FuncReadCoils            = 0x01
	FuncReadDiscreteInputs   = 0x02
	FuncReadHoldingRegisters = 0x03
	FuncReadInputRegisters   = 0x04
	FuncWriteSingleCoil      = 0x05
	FuncWriteSingleRegister  = 0x06
	FuncWriteMultipleCoils   = 0x0F
	FuncWriteMultipleRegs    = 0x10
)

// Exception codes
const (
	ExcIllegalFunction    = 0x01
	ExcIllegalDataAddress  = 0x02
	ExcIllegalDataValue    = 0x03
	ExcServerDeviceFailure = 0x04
)

// RegisterBank holds modbus data.
type RegisterBank struct {
	mu       sync.RWMutex
	Coils    []bool
	Discrete []bool
	Holding  []uint16
	Input    []uint16
	size     int
}

func NewBank(size int) *RegisterBank {
	if size <= 0 {
		size = 65536
	}
	return &RegisterBank{
		Coils:    make([]bool, size),
		Discrete: make([]bool, size),
		Holding:  make([]uint16, size),
		Input:    make([]uint16, size),
		size:     size,
	}
}

func (b *RegisterBank) valid(addr, qty int) bool {
	return addr >= 0 && qty > 0 && addr+qty <= b.size
}

// Client

type Client struct {
	conn    net.Conn
	addr    string
	mu      sync.Mutex
	tid     uint16
	timeout time.Duration
	unit    byte
}

func NewClient(addr string, timeout time.Duration, unit byte) *Client {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if unit == 0 {
		unit = 1
	}
	return &Client{addr: addr, timeout: timeout, unit: unit}
}

func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return nil
	}
	conn, err := net.DialTimeout("tcp", c.addr, c.timeout)
	if err != nil {
		return err
	}
	c.conn = conn
	return nil
}

func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

func (c *Client) tidNext() uint16 {
	c.tid++
	if c.tid == 0 {
		c.tid = 1
	}
	return c.tid
}

func (c *Client) transact(pdu []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		conn, err := net.DialTimeout("tcp", c.addr, c.timeout)
		if err != nil {
			return nil, err
		}
		c.conn = conn
	}
	conn := c.conn
	tid := c.tidNext()
	header := make([]byte, 7)
	binary.BigEndian.PutUint16(header[0:], tid)
	// protocol id 0
	binary.BigEndian.PutUint16(header[4:], uint16(len(pdu)+1))
	header[6] = c.unit
	// assemble
	buf := append([]byte{}, header...)
	buf = append(buf, pdu...)
	conn.SetDeadline(time.Now().Add(c.timeout))
	if _, err := conn.Write(buf); err != nil {
		conn.Close()
		c.conn = nil
		return nil, err
	}
	// read mbap
	mbap := make([]byte, 6)
	if _, err := io.ReadFull(conn, mbap); err != nil {
		conn.Close()
		c.conn = nil
		return nil, err
	}
	tidR := binary.BigEndian.Uint16(mbap[0:])
	_ = tidR
	length := binary.BigEndian.Uint16(mbap[4:])
	if length == 0 || length > 256+3 {
		return nil, errors.New("bad length")
	}
	rest := make([]byte, length)
	if _, err := io.ReadFull(conn, rest); err != nil {
		conn.Close()
		c.conn = nil
		return nil, err
	}
	// rest[0] unit id
	pduResp := rest[1:]
	if len(pduResp) > 0 && pduResp[0]&0x80 != 0 {
		if len(pduResp) >= 2 {
			return nil, fmt.Errorf("modbus exception %d", pduResp[1])
		}
		return nil, errors.New("modbus exception")
	}
	return pduResp, nil
}

func (c *Client) ReadHoldingRegisters(addr, qty uint16) ([]uint16, error) {
	pdu := []byte{FuncReadHoldingRegisters, byte(addr >> 8), byte(addr), byte(qty >> 8), byte(qty)}
	resp, err := c.transact(pdu)
	if err != nil {
		return nil, err
	}
	if len(resp) < 2 || resp[0] != FuncReadHoldingRegisters || int(resp[1]) != int(qty)*2 {
		return nil, errors.New("bad response")
	}
	out := make([]uint16, qty)
	for i := 0; i < int(qty); i++ {
		out[i] = binary.BigEndian.Uint16(resp[2+i*2:])
	}
	return out, nil
}

func (c *Client) ReadInputRegisters(addr, qty uint16) ([]uint16, error) {
	pdu := []byte{FuncReadInputRegisters, byte(addr >> 8), byte(addr), byte(qty >> 8), byte(qty)}
	resp, err := c.transact(pdu)
	if err != nil {
		return nil, err
	}
	if len(resp) < 2 || resp[0] != FuncReadInputRegisters || int(resp[1]) != int(qty)*2 {
		return nil, errors.New("bad response")
	}
	out := make([]uint16, qty)
	for i := 0; i < int(qty); i++ {
		out[i] = binary.BigEndian.Uint16(resp[2+i*2:])
	}
	return out, nil
}

func (c *Client) ReadCoils(addr, qty uint16) ([]bool, error) {
	pdu := []byte{FuncReadCoils, byte(addr >> 8), byte(addr), byte(qty >> 8), byte(qty)}
	resp, err := c.transact(pdu)
	if err != nil {
		return nil, err
	}
	if len(resp) < 2 {
		return nil, errors.New("bad response")
	}
	byteCount := int(resp[1])
	out := make([]bool, qty)
	for i := 0; i < int(qty); i++ {
		bi := i / 8
		if bi < byteCount {
			out[i] = (resp[2+bi]>>(uint(i)%8))&1 == 1
		}
	}
	return out, nil
}

func (c *Client) WriteSingleRegister(addr uint16, value uint16) error {
	pdu := []byte{FuncWriteSingleRegister, byte(addr >> 8), byte(addr), byte(value >> 8), byte(value)}
	resp, err := c.transact(pdu)
	if err != nil {
		return err
	}
	if len(resp) < 4 {
		return errors.New("bad response")
	}
	return nil
}

func (c *Client) WriteSingleCoil(addr uint16, on bool) error {
	val := uint16(0x0000)
	if on {
		val = 0xFF00
	}
	pdu := []byte{FuncWriteSingleCoil, byte(addr >> 8), byte(addr), byte(val >> 8), byte(val)}
	resp, err := c.transact(pdu)
	if err != nil {
		return err
	}
	if len(resp) < 4 {
		return errors.New("bad response")
	}
	return nil
}

func (c *Client) WriteMultipleRegisters(addr uint16, values []uint16) error {
	qty := len(values)
	pdu := []byte{FuncWriteMultipleRegs, byte(addr >> 8), byte(addr), byte(qty >> 8), byte(qty), byte(qty * 2)}
	for _, v := range values {
		pdu = append(pdu, byte(v>>8), byte(v))
	}
	_, err := c.transact(pdu)
	return err
}

// Server

type Server struct {
	bank   *RegisterBank
	ln     net.Listener
	addr   string
	mu     sync.Mutex
	conns  map[net.Conn]struct{}
	done   chan struct{}
}

func NewServer(addr string, bank *RegisterBank) *Server {
	if bank == nil {
		bank = NewBank(65536)
	}
	return &Server{addr: addr, bank: bank, conns: map[net.Conn]struct{}{}, done: make(chan struct{})}
}

func (s *Server) Bank() *RegisterBank { return s.bank }

func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.done:
				return nil
			default:
			}
			return err
		}
		s.mu.Lock()
		s.conns[conn] = struct{}{}
		s.mu.Unlock()
		go s.handleConn(conn)
	}
}

func (s *Server) Shutdown() {
	close(s.done)
	s.mu.Lock()
	if s.ln != nil {
		s.ln.Close()
	}
	for c := range s.conns {
		c.Close()
	}
	s.mu.Unlock()
}

func (s *Server) handleConn(conn net.Conn) {
	defer func() {
		conn.Close()
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
	}()
	for {
		conn.SetDeadline(time.Now().Add(60 * time.Second))
		mbap := make([]byte, 6)
		if _, err := io.ReadFull(conn, mbap); err != nil {
			return
		}
		length := binary.BigEndian.Uint16(mbap[4:])
		if length == 0 || length > 260 {
			return
		}
		rest := make([]byte, length)
		if _, err := io.ReadFull(conn, rest); err != nil {
			return
		}
		unit := rest[0]
		_ = unit
		pdu := rest[1:]
		respPdu := s.handlePDU(pdu)
		// build response
		tid := mbap[0:2]
		resp := append([]byte{}, tid...)
		resp = append(resp, 0, 0) // protocol id
		// length = 1 + len(respPdu)
		l := uint16(len(respPdu) + 1)
		resp = append(resp, byte(l>>8), byte(l))
		resp = append(resp, unit)
		resp = append(resp, respPdu...)
		conn.SetDeadline(time.Now().Add(10 * time.Second))
		if _, err := conn.Write(resp); err != nil {
			return
		}
	}
}

func (s *Server) handlePDU(pdu []byte) []byte {
	if len(pdu) == 0 {
		return excResp(0, ExcIllegalFunction)
	}
	fc := pdu[0]
	switch fc {
	case FuncReadCoils, FuncReadDiscreteInputs:
		if len(pdu) < 5 {
			return excResp(fc, ExcIllegalDataValue)
		}
		addr := binary.BigEndian.Uint16(pdu[1:])
		qty := binary.BigEndian.Uint16(pdu[3:])
		if qty == 0 || qty > 2000 {
			return excResp(fc, ExcIllegalDataValue)
		}
		s.bank.mu.RLock()
		var src []bool
		if fc == FuncReadCoils {
			src = s.bank.Coils
		} else {
			src = s.bank.Discrete
		}
		if !s.bank.valid(int(addr), int(qty)) {
			s.bank.mu.RUnlock()
			return excResp(fc, ExcIllegalDataAddress)
		}
		byteCount := (int(qty) + 7) / 8
		out := []byte{fc, byte(byteCount)}
		for i := 0; i < byteCount; i++ {
			out = append(out, 0)
		}
		for i := 0; i < int(qty); i++ {
			if src[int(addr)+i] {
				out[2+i/8] |= 1 << (uint(i) % 8)
			}
		}
		s.bank.mu.RUnlock()
		return out

	case FuncReadHoldingRegisters, FuncReadInputRegisters:
		if len(pdu) < 5 {
			return excResp(fc, ExcIllegalDataValue)
		}
		addr := binary.BigEndian.Uint16(pdu[1:])
		qty := binary.BigEndian.Uint16(pdu[3:])
		if qty == 0 || qty > 125 {
			return excResp(fc, ExcIllegalDataValue)
		}
		s.bank.mu.RLock()
		var src []uint16
		if fc == FuncReadHoldingRegisters {
			src = s.bank.Holding
		} else {
			src = s.bank.Input
		}
		if !s.bank.valid(int(addr), int(qty)) {
			s.bank.mu.RUnlock()
			return excResp(fc, ExcIllegalDataAddress)
		}
		out := []byte{fc, byte(qty * 2)}
		for i := 0; i < int(qty); i++ {
			v := src[int(addr)+i]
			out = append(out, byte(v>>8), byte(v))
		}
		s.bank.mu.RUnlock()
		return out

	case FuncWriteSingleCoil:
		if len(pdu) < 5 {
			return excResp(fc, ExcIllegalDataValue)
		}
		addr := binary.BigEndian.Uint16(pdu[1:])
		val := binary.BigEndian.Uint16(pdu[3:])
		if val != 0x0000 && val != 0xFF00 {
			return excResp(fc, ExcIllegalDataValue)
		}
		s.bank.mu.Lock()
		if int(addr) >= s.bank.size {
			s.bank.mu.Unlock()
			return excResp(fc, ExcIllegalDataAddress)
		}
		s.bank.Coils[addr] = (val == 0xFF00)
		s.bank.mu.Unlock()
		return append([]byte{}, pdu...) // echo

	case FuncWriteSingleRegister:
		if len(pdu) < 5 {
			return excResp(fc, ExcIllegalDataValue)
		}
		addr := binary.BigEndian.Uint16(pdu[1:])
		val := binary.BigEndian.Uint16(pdu[3:])
		s.bank.mu.Lock()
		if int(addr) >= s.bank.size {
			s.bank.mu.Unlock()
			return excResp(fc, ExcIllegalDataAddress)
		}
		s.bank.Holding[addr] = val
		s.bank.mu.Unlock()
		return append([]byte{}, pdu...)

	case FuncWriteMultipleCoils:
		if len(pdu) < 6 {
			return excResp(fc, ExcIllegalDataValue)
		}
		addr := binary.BigEndian.Uint16(pdu[1:])
		qty := binary.BigEndian.Uint16(pdu[3:])
		byteCount := int(pdu[5])
		if qty == 0 || qty > 1968 || byteCount != (int(qty)+7)/8 || len(pdu) < 6+byteCount {
			return excResp(fc, ExcIllegalDataValue)
		}
		s.bank.mu.Lock()
		if !s.bank.valid(int(addr), int(qty)) {
			s.bank.mu.Unlock()
			return excResp(fc, ExcIllegalDataAddress)
		}
		for i := 0; i < int(qty); i++ {
			s.bank.Coils[int(addr)+i] = (pdu[6+i/8]>>(uint(i)%8))&1 == 1
		}
		s.bank.mu.Unlock()
		return []byte{fc, byte(addr >> 8), byte(addr), byte(qty >> 8), byte(qty)}

	case FuncWriteMultipleRegs:
		if len(pdu) < 6 {
			return excResp(fc, ExcIllegalDataValue)
		}
		addr := binary.BigEndian.Uint16(pdu[1:])
		qty := binary.BigEndian.Uint16(pdu[3:])
		byteCount := int(pdu[5])
		if qty == 0 || qty > 123 || byteCount != int(qty)*2 || len(pdu) < 6+byteCount {
			return excResp(fc, ExcIllegalDataValue)
		}
		s.bank.mu.Lock()
		if !s.bank.valid(int(addr), int(qty)) {
			s.bank.mu.Unlock()
			return excResp(fc, ExcIllegalDataAddress)
		}
		for i := 0; i < int(qty); i++ {
			s.bank.Holding[int(addr)+i] = binary.BigEndian.Uint16(pdu[6+i*2:])
		}
		s.bank.mu.Unlock()
		return []byte{fc, byte(addr >> 8), byte(addr), byte(qty >> 8), byte(qty)}

	default:
		return excResp(fc, ExcIllegalFunction)
	}
}

func excResp(fc byte, code byte) []byte {
	return []byte{fc | 0x80, code}
}
