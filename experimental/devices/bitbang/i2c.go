// Copyright 2016 The Periph Authors. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Specification
//
// http://www.nxp.com/documents/user_manual/UM10204.pdf

package bitbang

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"

	"periph.io/x/periph/conn/gpio"
	//"periph.io/x/periph/conn/i2c"
	"periph.io/x/periph/conn/physic"
	//"periph.io/x/periph/host/cpu"
)

// SkipAddr can be used to skip the address from being sent.
const SkipAddr uint16 = 0xFFFF

// New returns an object that communicates I²C over two pins.
//
// BUG(maruel): It is close to working but not yet, the signal is incorrect
// during ACK.
//
// It has two special features:
// - Special address SkipAddr can be used to skip the address from being
//   communicated
// - An arbitrary speed can be used
func New(clk gpio.PinIO, data gpio.PinIO, f physic.Frequency) (*I2C, error) {
	// Spec calls to idle at high. Page 8, section 3.1.1.
	// Set SCL as pull-up.
	if err := clk.In(gpio.PullUp, gpio.NoEdge); err != nil {
		return nil, err
	}
	if err := clk.Out(gpio.High); err != nil {
		return nil, err
	}
	// Set SDA as pull-up.
	if err := data.In(gpio.PullUp, gpio.NoEdge); err != nil {
		return nil, err
	}
	if err := data.Out(gpio.High); err != nil {
		return nil, err
	}
	i := &I2C{
		scl:       clk,
		sda:       data,
		halfCycle: f.Period() / 2,
	}
	return i, nil
}

// Emulate open drain I/O
// Set gpio to input and let external pull up pull it high for 1
// Set gpio to output and drive low for 0
func (i *I2C) writeSdaOpenDrain(b bool) error {
	var err error
	if b {
		err = i.sda.In(gpio.PullUp, gpio.NoEdge)
	} else {
		err = i.sda.Out(false)
	}
	return err
}

func (i *I2C) writeSclOpenDrain(b bool) error {
	var err error
	if b {
		err = i.scl.In(gpio.PullUp, gpio.NoEdge)
	} else {
		err = i.scl.Out(false)
	}
	return err
}

// I2C represents an I²C master implemented as bit-banging on 2 GPIO pins.
type I2C struct {
	mu        sync.Mutex
	scl       gpio.PinIO // Clock line
	sda       gpio.PinIO // Data line
	halfCycle time.Duration
}

func (i *I2C) String() string {
	return fmt.Sprintf("bitbang/i2c(%s, %s)", i.scl, i.sda)
}

// Close implements i2c.BusCloser.
func (i *I2C) Close() error {
	return nil
}

// Tx implements i2c.Bus.
func (i *I2C) Tx(addr uint16, w, r []byte) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	//syscall.Setpriority(which, who, prio)

	i.start()
	defer i.stop()
	if addr != SkipAddr {
		if addr > 0xFF {
			// Page 15, section 3.1.11 10-bit addressing
			// TODO(maruel): Implement if desired; prefix 0b11110xx.
			return errors.New("bitbang-i2c: invalid address")
		}
		// Page 13, section 3.1.10 The slave address and R/W bit
		addr <<= 1
		if len(r) == 0 {
			addr |= 1
		}
		ack, err := i.writeByte(byte(addr))
		if err != nil {
			return err
		}
		if !ack {
			return errors.New("bitbang-i2c: got NACK")
		}
	}
	for _, b := range w {
		ack, err := i.writeByte(b)
		if err != nil {
			return err
		}
		if !ack {
			return errors.New("bitbang-i2c: got NACK")
		}
	}
	for x := range r {
		r[x] = i.readByte()

	}
	return nil
}

// w is a slice of bytes that holds the register value to be read from the i2c device
// r is a slice of bytes that holds bytes read back from the device
// readLen is a unint16 that specifies how many bytes to read back from the device

func (i *I2C) ReadRepeatedStart(addr uint16, w, r []byte) error {

	i.mu.Lock()
	defer i.mu.Unlock()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	//syscall.Setpriority(which, who, prio)

	// Battery sleeps so wake it up
	i.start()
	i.sleepHalfCycle()
	i.stop()
	i.sleepHalfCycle()

	i.start()
	defer i.stop()

	if addr != SkipAddr {
		if addr > 0xFF {
			// Page 15, section 3.1.11 10-bit addressing
			// TODO(maruel): Implement if desired; prefix 0b11110xx.
			return errors.New("bitbang-i2c: invalid address")
		}
		// Page 13, section 3.1.10 The slave address and R/W bit
		addr <<= 1

		ack, err := i.writeByte(byte(addr))
		if err != nil {
			return err
		}
		if !ack {
			return errors.New("bitbang-i2c: got NACK")
		}
	}

	for _, b := range w {
		ack, err := i.writeByte(b)
		if err != nil {
			return err
		}
		if !ack {
			return errors.New("bitbang-i2c: got NACK")
		}
	}

	// Here is the extra start needed to start reading data from the chip
	i.start()

	if addr != SkipAddr {
		if addr > 0xFF {
			// Page 15, section 3.1.11 10-bit addressing
			// TODO(maruel): Implement if desired; prefix 0b11110xx.
			return errors.New("bitbang-i2c: invalid address")
		}
		// Page 13, section 3.1.10 The slave address and R/W bit
		// Address was already shifted above, don't shift it again
		// Set read bit
		addr |= 1

		ack, err := i.writeByte(byte(addr))
		if err != nil {
			return err
		}
		if !ack {
			return errors.New("bitbang-i2c: got NACK")
		}
	}

	for x := range r {
		r[x] = i.readByte()

	}

	return nil
}

// SetSpeed implements i2c.Bus.
func (i *I2C) SetSpeed(f physic.Frequency) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.halfCycle = f.Period() / 2
	return nil
}

// SCL implements i2c.Pins.
func (i *I2C) SCL() gpio.PinIO {
	return i.scl
}

// SDA implements i2c.Pins.
func (i *I2C) SDA() gpio.PinIO {
	return i.sda
}

//

// "When CLK is a high level and DIO changes from high to low level, data input
// starts."
//
// Ends with SDA and SCL low.
//
// Lasts 1/2 cycle.
func (i *I2C) start() {
	// Page 9, section 3.1.4 START and STOP conditions
	// In multi-master mode, it would have to sense SDA first and after the sleep.

	// Must start with SCL and SDA high
	i.writeSdaOpenDrain(true)
	i.writeSclOpenDrain(true)

	//_ = i.sda.Out(gpio.Low)
	i.writeSdaOpenDrain(false)
	i.sleepHalfCycle()

	//_ = i.scl.Out(gpio.Low)
	i.writeSclOpenDrain(false)

}

// "When CLK is a high level and DIO changes from low level to high level, data
// input ends."
//
// Lasts 3/2 cycle.
func (i *I2C) stop() {
	// Page 9, section 3.1.4 START and STOP conditions
	_ = i.scl.Out(gpio.Low)
	i.sleepHalfCycle()
	_ = i.scl.Out(gpio.High)
	i.sleepHalfCycle()
	_ = i.sda.Out(gpio.High)
	// TODO(maruel): This sleep could be skipped, assuming we wait for the next
	// transfer if too quick to happen.
	i.sleepHalfCycle()
}

// writeByte writes 8 bits then waits for ACK.
//
// Expects SDA and SCL low.
//
// Ends with SDA low and SCL high.
//
// Lasts 9 cycles.
func (i *I2C) writeByte(b byte) (bool, error) {
	// Page 9, section 3.1.3 Data validity
	// "The data on te SDA line must be stable during the high period of the
	// clock."
	// Page 10, section 3.1.5 Byte format

	i.sleepHalfCycle()

	for x := 0; x < 8; x++ {
		i.writeSdaOpenDrain(b&byte(1<<byte(7-x)) != 0)
		i.sleepHalfCycle()
		i.writeSclOpenDrain(true)
		i.sleepHalfCycle()
		i.writeSclOpenDrain(false)
	}
	// Page 10, section 3.1.6 ACK and NACK
	// 9th clock is ACK.

	i.sleepHalfCycle()

	i.writeSclOpenDrain(true)

	i.writeSdaOpenDrain(true)

	// Implement clock stretching, the device may keep the line low.
	for i.scl.Read() == gpio.Low {
		i.sleepHalfCycle()
	}
	// ACK == Low.
	ack := i.sda.Read() == gpio.Low

	i.sleepHalfCycle()

	i.writeSclOpenDrain(false)

	//i.writeSdaOpenDrain(false)

	i.sleepHalfCycle()

	return ack, nil
}

// readByte reads 8 bits and an ACK.
//
// Lasts 9 cycles.
func (i *I2C) readByte() byte {

	var b byte

	i.sda.In(gpio.PullUp, gpio.NoEdge)

	for x := 0; x < 8; x++ {
		i.sleepHalfCycle()
		// TODO(maruel): Support clock stretching, the device may keep the line low.
		//_ = i.scl.Out(gpio.High)
		i.writeSclOpenDrain(true)
		i.sleepHalfCycle()
		if i.sda.Read() == gpio.High {
			b |= byte(1) << byte(7-x)
		}
		//_ = i.scl.Out(gpio.Low)
		i.writeSclOpenDrain(false)
	}

	i.sleepHalfCycle()

	i.writeSdaOpenDrain(false)
	i.writeSclOpenDrain(true)

	i.sleepHalfCycle()

	return b
}

// sleep does a busy loop to act as fast as possible.
func (i *I2C) sleepHalfCycle() {
	time.Sleep(time.Microsecond)
	return
	//cpu.Nanospin(i.halfCycle)
}

//var _ i2c.Bus = &I2C{}
