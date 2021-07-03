package label

import (
	_ "embed"
	"log"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
)

//go:embed fonts/mplus-1m-medium.ttf
var MPlus1mMedium []byte

var MPlus1mMediumFont *truetype.Font

// in order to avoid the repetitive loading of the font, we load it once
// during initalization into memory
func init() {
	var err error

	MPlus1mMedium, err = freetype.ParseFont(MPlus1mMedium)
	if err != nil {
		log.Panic(err)
	}
}
