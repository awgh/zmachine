package main

import (
	"io/ioutil"

	"github.com/awgh/zmachine"
)

func main() {
	buffer, err := ioutil.ReadFile("zork1.dat")
	if err != nil {
		panic(err)
	}

	var header zmachine.ZHeader
	header.Read(buffer)

	if header.Version != 3 {
		panic("Only Version 3 files supported")
	}

	var zm zmachine.ZMachine
	zm.Initialize(buffer, header)

	for !zm.Done {
		zm.InterpretInstruction()
	}
}
