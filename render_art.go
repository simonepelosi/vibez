//go:build ignore

package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/simone-vibes/vibez/internal/tui/art"
)

func main() {
	path := "internal/tui/art/testdata/gorillaz_2001_album.png"
	width := 32
	height := 16

	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	if len(os.Args) > 2 {
		v, err := strconv.Atoi(os.Args[2])
		if err != nil {
			panic(err)
		}
		width = v
	}
	if len(os.Args) > 3 {
		v, err := strconv.Atoi(os.Args[3])
		if err != nil {
			panic(err)
		}
		height = v
	}

	f, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	img, err := art.Decode(f)
	if err != nil {
		panic(err)
	}

	for _, line := range art.RenderHalfBlocks(img, art.Size{Width: width, Height: height}) {
		fmt.Println(line)
	}
}
