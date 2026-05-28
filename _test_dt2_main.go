//go:build ignore

package main

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/g-lok/rexconverter/internal/rexengine"
)

func main() {
	ext := &rexengine.SliceExtraction{
		Interleaved: make([]float32, 48000),
		CuePoints: []rexengine.CuePoint{
			{Position: 0},
			{Position: 12000},
			{Position: 24000},
			{Position: 36000},
		},
		Metadata: rexengine.AudioMetadata{
			SampleRate: 48000,
			Channels:   1,
		},
	}

	var buf bytes.Buffer
	err := rexengine.EncodeDT2Preset(&buf, ext)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}

	data := buf.Bytes()
	fmt.Printf("File size: %d bytes\n", len(data))
	fmt.Printf("Signature: %x\n", data[:4])

	pos := 0
	i := 0
	for pos < len(data)-22 && i < 10 {
		if len(data) < pos+30 {
			break
		}
		sig := data[pos:pos+4]
		if string(sig) != "PK\x03\x04" {
			pos++
			continue
		}
		flags := uint16(data[pos+6]) | uint16(data[pos+7])<<8
		method := uint16(data[pos+8]) | uint16(data[pos+9])<<8
		crc32_ := uint32(data[pos+14]) | uint32(data[pos+15])<<8 | uint32(data[pos+16])<<16 | uint32(data[pos+17])<<24
		compSize := uint32(data[pos+18]) | uint32(data[pos+19])<<8 | uint32(data[pos+20])<<16 | uint32(data[pos+21])<<24
		uncompSize := uint32(data[pos+22]) | uint32(data[pos+23])<<8 | uint32(data[pos+24])<<16 | uint32(data[pos+25])<<24
		nameLen := uint16(data[pos+26]) | uint16(data[pos+27])<<8
		extraLen := uint16(data[pos+28]) | uint16(data[pos+29])<<8
		name := string(data[pos+30 : pos+30+int(nameLen)])

		fmt.Printf("\nEntry %d: %s\n", i, name)
		fmt.Printf("  Flags: %d (data descriptor=%v)\n", flags, flags&0x8 != 0)
		fmt.Printf("  Method: %d\n", method)
		fmt.Printf("  CRC32: %08x\n", crc32_)
		fmt.Printf("  Compressed: %d, Uncompressed: %d\n", compSize, uncompSize)

		pos += 30 + int(nameLen) + int(extraLen) + int(compSize)
		i++
	}

	hasDataDesc := strings.Contains(string(data), "PK\a\b")
	fmt.Printf("\nHas data descriptors: %v\n", hasDataDesc)
	fmt.Printf("Entries found: %d\n", i)
	
	// Parse binary
	sig2 := strings.Contains(string(data), "PK\x05\x06")
	fmt.Printf("Has EOCD: %v\n", sig2)
}
