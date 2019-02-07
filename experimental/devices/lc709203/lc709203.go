package main

import (
	"fmt"

	"../bitbang"
	"periph.io/x/periph/conn/gpio/gpioreg"
	"periph.io/x/periph/conn/physic"
	"periph.io/x/periph/host"
)

func main() {

	// Sequence to read RSOC
	// See data sheet at https://www.onsemi.com/PowerSolutions/product.do?id=LC709203F
	// Start Write 0B 0D Start Read 1 Byte Stop
	// First byte read is the RSOC (relative state of charge) number

	host.Init()

	scl := gpioreg.ByName("GPIO24")
	sda := gpioreg.ByName("GPIO23")
	freq := physic.Frequency(10000)

	i2cBus, err := bitbang.New(scl, sda, freq)
	if err != nil {
		fmt.Println("Error instantiating I2C buss pins")
	}

	defer i2cBus.Close()

	//fmt.Println(i2cBus.String())

	rsocAddr := make([]byte, 1)
	rsocAddr[0] = 0x0D

	batMonData := make([]byte, 1)

	err = i2cBus.ReadRepeatedStart(0x0B, rsocAddr, batMonData)
	if err != nil {
		fmt.Println("I2C repeated start failed with error: ", err)
	} else {
		fmt.Printf("%d", batMonData[0])
	}

}
