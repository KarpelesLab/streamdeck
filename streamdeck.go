//go:generate stringer -type=BtnState

package streamdeck

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"log"
	"os"
	"sync"
	"time"

	"github.com/disintegration/gift"
	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"

	"github.com/KarpelesLab/hid"

	"image/color"
	"image/draw"
	_ "image/gif"  // support gif
	_ "image/jpeg" // support jpeg
	_ "image/png"  // support png
)

// VendorID is the USB VendorID assigned to Elgato (0x0fd9)
const VendorID = 4057

// NumButtons is the total amount of Buttons located on the Stream Deck.
const NumButtons = 15

// ButtonSize is the size of a button (in pixel).
const ButtonSize = 80

// NumButtonColumns is the number of columns on the Stream Deck.
const NumButtonColumns = 5

// NumButtonRows is the number of button rows on the Stream Deck.
const NumButtonRows = 3

// Spacer is the spacing distance (in pixel) of two buttons on the Stream Deck.
const Spacer = 19

// PanelWidth is the total screen width of the Stream Deck (including spacers).
const PanelWidth = NumButtonColumns*ButtonSize + Spacer*(NumButtonColumns-1)

// PanelHeight is the total screen height of the stream deck (including spacers).
const PanelHeight = NumButtonRows*ButtonSize + Spacer*(NumButtonRows-1)

// BtnEvent is a callback which gets executed when the state of a button changes,
// so whenever it gets pressed or released.
type BtnEvent func(btnIndex int, newBtnState BtnState)

// BtnState is a type representing the button state.
type BtnState int

const (
	// BtnPressed button pressed
	BtnPressed BtnState = iota
	// BtnReleased button released
	BtnReleased
)

// ReadErrorCb is a callback which gets executed in case reading from the
// Stream Deck fails (e.g. the cable get's disconnected).
type ReadErrorCb func(err error)

// StreamDeck is the object representing the Elgato Stream Deck.
type StreamDeck struct {
	sync.Mutex
	device     hid.Handle
	btnEventCb BtnEvent
	btnState   []BtnState
	info       *StreamdeckDevice
}

// TextButton holds the lines to be written to a button and the desired
// Background color.
type TextButton struct {
	Lines   []TextLine
	BgColor color.Color
}

// TextLine holds the content of one text line.
type TextLine struct {
	Text      string
	PosX      int
	PosY      int
	Font      *truetype.Font
	FontSize  float64
	FontColor color.Color
}

// Page contains the configuration of one particular page of buttons. Pages
// can be nested to an arbitrary depth.
type Page interface {
	Set(btnIndex int, state BtnState) Page
	Parent() Page
	Draw()
	SetActive(bool)
}

// NewStreamDeck is the constructor of the StreamDeck object. If several StreamDecks
// are connected to this PC, the Streamdeck can be selected by supplying
// the optional serial number of the Device. In the examples folder there is
// a small program which enumerates all available Stream Decks. If no serial number
// is supplied, the first StreamDeck found will be selected.
func NewStreamDeck(serial ...string) (*StreamDeck, error) {
	log.Printf("about to enumerate devices")

	if len(serial) > 1 {
		return nil, fmt.Errorf("only <= 1 serial numbers must be provided")
	}

	var devices []hid.Device
	hid.UsbWalk(func(device hid.Device) {
		info := device.Info()
		if info.Vendor != VendorID {
			return
		}

		found := false
		for _, sd := range streamdeckDevices {
			if sd.ProductID == info.Product {
				// found device
				devices = append(devices, device)
				found = true
				break
			}
		}
		if !found {
			log.Printf("WARNING: unsupported Elgato device %04x:%04x:%04x:%02x", info.Vendor, info.Product, info.Revision, info.Interface)
		}
	})

	if len(devices) == 0 {
		return nil, fmt.Errorf("no stream deck device found")
	}

	id := 0
	/*
		if len(serial) == 1 {
			found := false
			for i, d := range devices {
				info := d.Info()
				if info.Serial == serial[0] {
					id = i
					found = true
				}
			}
			if !found {
				return nil, fmt.Errorf("no stream deck device found with serial number %s", serial[0])
			}
		}*/

	handle, err := devices[id].Open()
	if err != nil {
		return nil, err
	}

	info := devices[id].Info()
	var sdinfo *StreamdeckDevice
	for _, sdinfo = range streamdeckDevices {
		if sdinfo.ProductID == info.Product {
			break
		}
	}

	sd := &StreamDeck{
		device:   handle,
		btnState: make([]BtnState, NumButtons),
		info:     sdinfo,
	}

	// initialize buttons to state BtnReleased
	for i := range sd.btnState {
		sd.btnState[i] = BtnReleased
	}

	err = sd.Reset()
	if err != nil {
		return nil, err
	}
	sd.SetBrightness(100)
	sd.ClearAllBtns()

	go sd.read()

	return sd, nil
}

// SetBtnEventCb sets the BtnEvent callback which get's executed whenever
// a Button event (pressed/released) occures.
func (sd *StreamDeck) SetBtnEventCb(ev BtnEvent) {
	sd.Lock()
	defer sd.Unlock()
	sd.btnEventCb = ev
}

// Read will listen in a for loop for incoming messages from the Stream Deck.
// It is typically executed in a dedicated go routine.
func (sd *StreamDeck) read() {
	for {
		data, err := sd.device.ReadInputPacket(time.Second)
		if err != nil {
			continue
		}

		if data[0] != 1 {
			continue
		}

		data = data[1:] // strip off the first byte; usage unknown, but it is always '\x01'

		sd.Lock()
		// we have to iterate over all 15 buttons and check if the state
		// has changed. If it has changed, execute the callback.
		for i, b := range data {
			if i >= len(sd.btnState) {
				break
			}
			if sd.btnState[i] != itob(int(b)) {
				sd.btnState[i] = itob(int(b))
				if sd.btnEventCb != nil {
					btnState := sd.btnState[i]
					go sd.btnEventCb(i, btnState)
				}
			}
		}
		sd.Unlock()
	}
}

// Close the connection to the Elgato Stream Deck
func (sd *StreamDeck) Close() error {
	sd.Lock()
	sd.Unlock()
	return sd.device.Close()
}

// ClearBtn fills a particular key with the color black
func (sd *StreamDeck) ClearBtn(btnIndex int) error {
	//log.Printf("about to clear button %d", btnIndex)

	if err := checkValidKeyIndex(btnIndex); err != nil {
		return err
	}
	return sd.FillColor(btnIndex, 0, 0, 0)
}

// ClearAllBtns fills all keys with the color black
func (sd *StreamDeck) ClearAllBtns() {
	for i := sd.ButtonCount() - 1; i >= 0; i-- {
		sd.ClearBtn(i)
	}
}

func (sd *StreamDeck) ButtonCount() int {
	return sd.info.NumButtons
}

// FillColor fills the given button with a solid color.
func (sd *StreamDeck) FillColor(btnIndex, r, g, b int) error {

	if err := checkRGB(r); err != nil {
		return err
	}
	if err := checkRGB(g); err != nil {
		return err
	}
	if err := checkRGB(b); err != nil {
		return err
	}

	img := image.NewRGBA(image.Rect(0, 0, ButtonSize, ButtonSize))
	color := color.RGBA{uint8(r), uint8(g), uint8(b), 0}
	draw.Draw(img, img.Bounds(), image.NewUniform(color), image.Point{0, 0}, draw.Src)

	return sd.FillImage(btnIndex, img)
}

func makeBitmap(img image.Image, rotate int) []byte {
	for rotate < 0 {
		rotate += 360
	}

	width := img.Bounds().Dx()
	height := img.Bounds().Dy()

	switch rotate {
	case 90, 270:
		width, height = height, width
	}

	pixelSize := width * height * 3
	fileSize := pixelSize + 54 // header is 54 bytes long
	out := &bytes.Buffer{}

	out.Write([]byte{'B', 'M'})
	binary.Write(out, binary.LittleEndian, uint32(fileSize))
	out.Write([]byte{0, 0, 0, 0})                      // reserved
	binary.Write(out, binary.LittleEndian, uint32(54)) // starting offset of pixels (header size)

	// BITMAPINFOHEADER
	binary.Write(out, binary.LittleEndian, uint32(40)) // DIB header size
	binary.Write(out, binary.LittleEndian, uint32(width))
	binary.Write(out, binary.LittleEndian, uint32(height))
	binary.Write(out, binary.LittleEndian, uint16(1))  // The number of color planes, must be 1
	binary.Write(out, binary.LittleEndian, uint16(24)) // the number of bits per pixels
	binary.Write(out, binary.LittleEndian, uint32(0))  // compression method (BI_RGB)
	binary.Write(out, binary.LittleEndian, uint32(pixelSize))
	binary.Write(out, binary.LittleEndian, int32(3780)) // the horizontal resolution of the image. (pixel per metre, signed integer)
	binary.Write(out, binary.LittleEndian, int32(3780)) // the vertical resolution of the image. (pixel per metre, signed integer)
	binary.Write(out, binary.LittleEndian, uint32(0))   // the number of colors in the color palette (meaningless in RGB mode)
	binary.Write(out, binary.LittleEndian, uint32(0))   // the number of important colors used (generally ignored)

	// write pixels
	switch rotate {
	case 0:
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				out.Write([]byte{byte(b), byte(g), byte(r)})
			}
		}
	case 90:
		for x := 0; x < width; x++ {
			for y := 0; y < height; y++ {
				r, g, b, _ := img.At(x, height-y-1).RGBA()
				out.Write([]byte{byte(b), byte(g), byte(r)})
			}
		}
	case 180:
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				r, g, b, _ := img.At(width-x-1, height-y-1).RGBA()
				out.Write([]byte{byte(b), byte(g), byte(r)})
			}
		}
	case 270:
		for x := 0; x < width; x++ {
			for y := 0; y < height; y++ {
				r, g, b, _ := img.At(width-x-1, y).RGBA()
				out.Write([]byte{byte(b), byte(g), byte(r)})
			}
		}
	}

	// end
	return out.Bytes()
}

// FillImage fills the given key with an image. For best performance, provide
// the image in the size of ?x? pixels. Otherwise it will be automatically
// resized.
func (sd *StreamDeck) FillImage(btnIndex int, img image.Image) error {
	if err := checkValidKeyIndex(btnIndex); err != nil {
		return err
	}

	imgBuf := makeBitmap(img, 270)

	sd.Lock()
	defer sd.Unlock()

	return sd.writeBitmap(uint8(btnIndex), imgBuf)
}

// FillImageFromFile fills the given key with an image from a file.
func (sd *StreamDeck) FillImageFromFile(keyIndex int, path string) error {
	reader, err := os.Open(path)
	if err != nil {
		return err
	}
	defer reader.Close()

	img, _, err := image.Decode(reader)
	if err != nil {
		return err
	}

	return sd.FillImage(keyIndex, img)
}

// FillPanel fills the whole panel witn an image. The image is scaled to fit
// and then center-cropped (if necessary). The native picture size is 360px x 216px.
func (sd *StreamDeck) FillPanel(img image.Image) error {

	// resize if the picture width is larger or smaller than panel
	rect := img.Bounds()
	if rect.Dx() != PanelWidth {
		newWidthRatio := float32(rect.Dx()) / float32((PanelWidth))
		img = resize(img, PanelWidth, int(float32(rect.Dy())/newWidthRatio))
	}

	// if the Canvas is larger than PanelWidth x PanelHeight then we crop
	// the Center match PanelWidth x PanelHeight
	rect = img.Bounds()
	if rect.Dx() > PanelWidth || rect.Dy() > PanelHeight {
		img = cropCenter(img, PanelWidth, PanelHeight)
	}

	counter := 0

	for row := 0; row < NumButtonRows; row++ {
		for col := 0; col < NumButtonColumns; col++ {
			rect := image.Rectangle{
				Min: image.Point{
					PanelWidth - ButtonSize - col*ButtonSize - col*Spacer,
					row*ButtonSize + row*Spacer,
				},
				Max: image.Point{
					PanelWidth - 1 - col*ButtonSize - col*Spacer,
					ButtonSize - 1 + row*ButtonSize + row*Spacer,
				},
			}
			sd.FillImage(counter, img.(*image.RGBA).SubImage(rect))
			counter++
		}
	}

	return nil
}

// FillPanelFromFile fills the entire panel with an image from a file.
func (sd *StreamDeck) FillPanelFromFile(path string) error {
	reader, err := os.Open(path)
	if err != nil {
		return err
	}
	defer reader.Close()

	img, _, err := image.Decode(reader)
	if err != nil {
		return err
	}

	return sd.FillPanel(img)
}

// WriteText can write several lines of Text to a button. It is up to the
// user to ensure that the lines fit properly on the button.
func (sd *StreamDeck) WriteText(btnIndex int, textBtn TextButton) error {

	if err := checkValidKeyIndex(btnIndex); err != nil {
		return err
	}

	img := image.NewRGBA(image.Rect(0, 0, ButtonSize, ButtonSize))
	bg := image.NewUniform(textBtn.BgColor)
	// fill button with Background color
	draw.Draw(img, img.Bounds(), bg, image.Point{0, 0}, draw.Src)

	for _, line := range textBtn.Lines {
		fontColor := image.NewUniform(line.FontColor)
		c := freetype.NewContext()
		c.SetDPI(72)
		c.SetFont(line.Font)
		c.SetFontSize(line.FontSize)
		c.SetClip(img.Bounds())
		c.SetDst(img)
		c.SetSrc(fontColor)
		pt := freetype.Pt(line.PosX, line.PosY+int(c.PointToFixed(24)>>6))

		if _, err := c.DrawString(line.Text, pt); err != nil {
			return err
		}
	}

	sd.FillImage(btnIndex, img)
	return nil
}

func (sd *StreamDeck) Reset() error {
	payload := make([]byte, 17)
	payload[0] = 0x0b
	payload[1] = 0x63

	return sd.device.SetFeatureReport(0, payload)
}

func (sd *StreamDeck) SetBrightness(pc uint8) error {
	payload := make([]byte, 17)
	payload[0] = 0x05
	payload[1] = 0x55
	payload[2] = 0xaa
	payload[3] = 0xd1
	payload[4] = 0x01
	payload[5] = pc

	return sd.device.SetFeatureReport(0, payload)
}

func (sd *StreamDeck) writeBitmap(key uint8, buf []byte) error {
	// write buf through interrupt, limit to 1024 bytes each time
	out := make([]byte, 1024)
	out[0] = 0x02
	out[1] = 0x01
	out[5] = key + 1

	page_no := uint8(0)

	//log.Printf("about to write %d bytes of data...", len(buf))

	for {
		out[2] = page_no
		page_no += 1
		copy(out[16:], buf)

		if len(buf) <= (len(out) - 16) {
			out[4] = 1 // eof
			buf = nil
		} else {
			buf = buf[len(out)-16:]
		}

		_, err := sd.device.Write(out, time.Second)
		//err := sd.device.SetReport(0x0202, out)
		if err != nil {
			panic(fmt.Sprintf("failed to setreport: %s", err))
		}
		//log.Printf("wrote %d bytes, remaining %d", len(out), len(buf))

		if len(buf) == 0 {
			return nil
		}
	}
}

// resize returns a resized copy of the supplied image with the given width and height.
func resize(img image.Image, width, height int) image.Image {
	g := gift.New(
		gift.Resize(width, height, gift.LanczosResampling),
		gift.UnsharpMask(1, 1, 0),
	)
	res := image.NewRGBA(g.Bounds(img.Bounds()))
	g.Draw(res, img)
	return res
}

// crop center will extract a sub image with the given width and height
// from the center of the supplied picture.
func cropCenter(img image.Image, width, height int) image.Image {
	g := gift.New(
		gift.CropToSize(width, height, gift.CenterAnchor),
	)
	res := image.NewRGBA(g.Bounds(img.Bounds()))
	g.Draw(res, img)
	return res
}

// checkValidKeyIndex checks that the keyIndex is valid
func checkValidKeyIndex(keyIndex int) error {
	if keyIndex < 0 || keyIndex > 15 {
		return fmt.Errorf("invalid key index")
	}
	return nil
}

// checkRGB returns an error in case of an invalid color (8 bit)
func checkRGB(value int) error {
	if value < 0 || value > 255 {
		return fmt.Errorf("invalid color range")
	}
	return nil
}

// int to ButtonState
func itob(i int) BtnState {
	if i == 0 {
		return BtnReleased
	}
	return BtnPressed
}
