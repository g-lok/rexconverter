package main

import "C"
import "github.com/g-lok/rexconverter/cmd"

//export GoMainEntry
func GoMainEntry() {
	cmd.Execute()
}

func main() {
	// Keep empty. The execution lifecycle is managed by our entry wrapper token function.
}
