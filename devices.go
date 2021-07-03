package streamdeck

type StreamdeckDevice struct {
	ProductID    uint16
	Name         string
	NumButtons   int
	ButtonSize   int
	StreamBuffer int
	Spacer       int
}

var streamdeckDevices = []*StreamdeckDevice{
	&StreamdeckDevice{
		ProductID:    0x0060, // legacy
		Name:         "Legacy Stream Deck",
		NumButtons:   15, // 5x3
		ButtonSize:   72,
		StreamBuffer: 8192, // ??? test me
		Spacer:       19,
	},
	&StreamdeckDevice{
		ProductID:    0x0063, // mini
		Name:         "Stream Deck Mini",
		NumButtons:   6, // 3x2
		ButtonSize:   80,
		StreamBuffer: 1024,
		Spacer:       19, // ?? is this value event relevant?
	},
}
