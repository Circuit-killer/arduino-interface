package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"time"

	//"github.com/chuckpreslar/emission"
	"github.com/gin-gonic/gin"
	"github.com/marcusolsson/tui-go"
	"github.com/tarm/serial"
)

// TODO:
// Make Message type that these inherent from ,or interface
// whatever is the golang way

type ChatMessage struct {
	Name string `json:"name"`
	Data string `json:"data"`
	Time string `json:"time"`
}

type PinMessage struct {
	Device   string `json:"device"`
	Active   int    `json:"active"`
	Number   int    `json:"number"`
	IsAnalog int    `json:"is_analog"`
	IsOutput int    `json:"is_output"`
	Current  int    `json:"current"`
	Target   int    `json:"target"`
	Velocity int    `json:"velocity"`
	Error    string `json:"error"`
}

type StatusMessage struct {
	Data PinMessage `json:"data"`
	Time int64      `json:"time"`
}

type Category struct {
	Name    string
	Sensors []Sensor
	Devices []Device
}

type Tag struct {
	Name    string
	Sensors []Sensor
	Devices []Device
}

type Sensor struct {
	Name       string
	Category   string
	Tags       []Tag
	IsAnalog   bool
	MinCurrent float32
	MaxCurrent float32
}

// Motors? Displays? Lights? Or abstract it in current structure?

type Device struct {
	Name          string   `json:"name"`
	IPAddress     string   `json:"ip_address"`
	WebAPI        bool     `json"web_api"`
	Category      Category `json:"category"`
	Sensors       []Sensor
	Tags          []Tag `json:"tags"`
	Baud          int
	Configuration *serial.Config
	Port          *serial.Port
	Connected     bool
	ReadBuffer    []byte
	StatusMessage PinMessage
}

type TextUI struct {
	Channels *tui.Box
	Messages *tui.Box
	Sidebar  *tui.Box
	History  *tui.Box
	Input    *tui.Box
	Entry    *tui.Entry
}

type state struct {
	Devices []Device
	// TODO: Need functionst to prune messagehuistory and chatbuffer to prevent it from
	// just going on forever. should be a config option
	MessageHistory []StatusMessage
	ChatHistory    []ChatMessage
	TUI            TextUI
	Configuration  configuration
	Help           bool `json:"help"`
}

type configuration struct {
	HostName         string `json:"name"`
	HostAddress      string `json:"host_addresss"`
	HostPort         string `json:"host_port"`
	MaxStatusHistory int
	MaxChatHistory   int
	Debug            bool `json:"debug_mode"`
}

var ChatMessages = []ChatMessage{
	{Name: "SYSTEM", Data: "Microcontroller Interface Initialized.", Time: time.Now().Format("15:04")},
}

func (state *state) scanForDevices() (device Device, err error) {
	device.Connected = false
	device.Name = findMicrocontroller()
	if len(device.Name) > 0 {
		log.Println("Found microcontroller: ", device.Name)
		log.Println("Attempting to connect to %s, at 57600 Baud: ", device.Name)

		device.Configuration = &serial.Config{Name: device.Name, Baud: 57600}
		// TODO: Check if record exists in configuration, if it does,set Baud to that section.
		// Otherwise determine the best Baud for the given device to maximize the ability
		// for the microcontroller to function as an interface.
		if err != nil {
			log.Println("Failed to connect to microcontroller, scanning for new devices.")
			err = errors.New("Failed to connect to microcontroller")
		} else {
			device.Port, err = serial.OpenPort(device.Configuration)
			if err != nil {
				log.Println("Failed to connect to microcontroller, scanning for new devices.")
			} else {
				defer device.Port.Close()
				state.Devices = append(state.Devices, device)
			}
		}
	} else {
		log.Println("No microcontrollers found. Scan will be retried after a short waiting period...")
		err = errors.New("Failed to find a microcontroller")
	}
	return device, err
}

func main() {
	// Initialize State
	state := &state{
		MessageHistory: []StatusMessage{},
		// TODO: Read from ~/.microcontroller-interface.yml, create if does not exist based on the defaults.
		Help: false,
		Configuration: configuration{
			HostName:         "localhost",
			HostAddress:      "127.0.0.1",
			HostPort:         "8783",
			MaxStatusHistory: 500,
			MaxChatHistory:   500,
			Debug:            false,
		},
	}

	// Apply Flags
	helpFlag := flag.Bool("help", false, "Provide a help dialog")
	debugFlag := flag.Bool("debug", false, "Enable debug mode")
	flag.StringVar(&state.Configuration.HostName, "name", "wall", "Provide a name for server configuration generation")
	flag.StringVar(&state.Configuration.HostAddress, "host", "0.0.0.0", "Provide an address to listen on")
	flag.StringVar(&state.Configuration.HostPort, "--port", "8783", "Provide a port number")
	flag.Parse()
	if &state.Help != helpFlag {
		log.Println("\n    ## Microcontroller Interface Server")
		log.Println("      Usage:")
		log.Println("      Please specify the server defined in the configuration to start:")
		log.Println("          lsrvr [options]")
		log.Println("      Options:")
		log.Println("          --name  [option]   # Provide a name, if none default name (wall) will be used")
		log.Println("          --host  [option]   # Provide a interface, if none default interface (0.0.0.0) will be used")
		log.Println("          --port  [option]   # Provide a port, if none default port (8783) will be used\n")
		log.Println("          --debug [option]   # Debug mode, if none default value (false) will be used\n")
		log.Println("      Example:")
		log.Println("          microcontroller-interface --host 10.0.0.5 --port 8080\n\n")
		os.Exit(0)
	}

	// Scan for microcontrollers

	// Initialize Emitter
	//emitter := emission.NewEmitter()

	// Web API
	if &state.Configuration.Debug != debugFlag {
		os.Setenv("GIN_MODE", "release")
	}

	// Initialize REST API
	r := gin.Default()
	r.GET("/", func(c *gin.Context) {
		c.JSON(200, state.MessageHistory)
	})
	r.POST("/", func(c *gin.Context) {
		var message PinMessage
		err := c.BindJSON(&message)
		if err != nil {
			log.Println("Error: Failed to parse JSON ", err)
		}
		// Valdiate JSON Data
		// Add a better curve for changing the values, the s-curve is better than
		// a linear curve. Slow Fast Slow, with a ramp up period and a ramp down.
		if message.Velocity < 1 {
			message.Velocity = 1
		}
		// Validate device
		message.Active = checkLimits(message.Active)
		message.Number = checkLimits(message.Number)
		message.IsAnalog = checkLimits(message.IsAnalog)
		message.IsOutput = checkLimits(message.IsOutput)
		message.Current = checkLimits(message.Current)
		message.Target = checkLimits(message.Target)
		message.Velocity = checkLimits(message.Velocity)
		// Pass Data To Microcontroller
		var pinMessage PinMessage
		// TODO:
		//  Each through all devices and send message, and have an option to find and send
		// a message to a single device. Also deviecs should have roles, or categories, then
		//  messages can just be sent to devices of that category.

		if len(state.Devices) > 0 {
			for _, device := range state.Devices {
				err = json.Unmarshal(state.passToSerial(device, message), &pinMessage)
				var response = &StatusMessage{
					Time: time.Now().Unix(),
					Data: pinMessage,
				}
				state.MessageHistory = append(state.MessageHistory, *response)
				// Serve Response
				c.JSON(200, response)
			}
		} else {
			pinMessage.Error = "Failed to parse serial stream."
			var response = &StatusMessage{
				Time: time.Now().Unix(),
				Data: pinMessage,
			}
			c.JSON(200, response)
		}
	})

	host := state.Configuration.HostAddress + ":" + state.Configuration.HostPort
	log.Println("Microcontroller Interface now listening on " + host + "\n")
	log.Println("Save the following to ~/.microcontroller-interface.yml if it does not already exist")
	log.Println("in order to use the microcontroller-interface command with this server.\n")
	log.Println("servers:")
	log.Println("  " + state.Configuration.HostName + ":")
	log.Println("    host_ip: " + state.Configuration.HostAddress)
	log.Println("    host_port: " + state.Configuration.HostPort)
	go r.Run(host)

	// DEVELOPMENT
	// Multidevice console, this will need to be split into its own client software
	if len(state.Devices) > 0 {
		for _, device := range state.Devices {
			go state.readFromSerial(device)
		}
	}
	state.textUI()
}

func checkLimits(value int) int {
	if value < 0 {
		return 0
	} else if value > 255 {
		return 255
	} else {
		return value
	}
}

func (state *state) passToSerial(device Device, message PinMessage) []byte {
	// Write JSON Command
	messageBytes, err := json.Marshal(message)
	_, err = device.Port.Write(messageBytes)
	if err != nil {
		log.Println("Error: Write error", err)
		return nil
	}
	device.ReadBuffer = state.readFromSerial(device)
	return device.ReadBuffer
}

func (msg PinMessage) asStatusString() string {
	return fmt.Sprintf("<%s> Status for PIN %v output: %v, towards %v at a velocity of %v.", msg.Device, msg.Number, msg.Current, msg.Target, msg.Velocity)
}

func (state *state) readFromSerial(device Device) []byte {
	// TODO: This should not wait for an entire message before moving forward.
	// Maybe not since its a go  func. But it then needs to call itself instead of looping.
	// Read JSON Response
	var readCount int
	byteBuffer := make([]byte, 8)
	for {
		n, err := device.Port.Read(byteBuffer)
		if err != nil {
			log.Println("Error: Read error", err)
			break
		}
		readCount++
		device.ReadBuffer = append(device.ReadBuffer, byteBuffer[:n]...)
		// TODO: Turn outputbytes into pinMessage or statusMessage then add it to messagehistory
		if bytes.Contains(byteBuffer[:n], []byte("\n")) {
			var m PinMessage
			m.Device = device.Name
			err = json.Unmarshal(device.ReadBuffer, m)
			b := tui.NewHBox(
				tui.NewLabel(time.Now().Format("15:04")),
				tui.NewPadder(1, 0, tui.NewLabel(fmt.Sprintf("<%s>", state.Configuration.HostName))),
				tui.NewLabel(fmt.Sprintf("%s", m.asStatusString)),
			)
			state.TUI.History.Append(b)
			// Stop at the termination of a JSON statement
			break
		} else if readCount > 15 {
			// Prevent from read lasting forever
			break
		}
	}
	return device.ReadBuffer
}

// findMicrocontroller looks for the file that represents the Microcontroller
// serial connection. Returns the fully qualified path to the
// device if we are able to find a likely candidate for an
// Microcontroller, otherwise an empty string if unable to find
// something that 'looks' like an Arduino device.
func findMicrocontroller() string {
	contents, _ := ioutil.ReadDir("/dev")
	// Look for what is mostly likely the Arduino device
	for _, f := range contents {
		if strings.Contains(f.Name(), "tty.usbserial") ||
			strings.Contains(f.Name(), "ttyUSB") ||
			strings.Contains(f.Name(), "ttyACM") {
			return "/dev/" + f.Name()
		}
	}
	// Have not been able to find a USB device that 'looks'
	// like an Arduino.
	return ""
}

func (state *state) textUI() {
	channels := tui.NewVBox(
		tui.NewLabel("All"),
		tui.NewLabel("Lights"),
		tui.NewLabel("PowerOutlets"),
		tui.NewLabel("Heaters"),
	)

	peers := tui.NewVBox(
		tui.NewLabel("wall-light"),
		tui.NewLabel("desk-light"),
	)

	sidebar := tui.NewVBox(
		tui.NewLabel("GROUPS"),
		channels,
		tui.NewLabel(""),
		tui.NewLabel("P2P"),
		peers,
	)
	sidebar.SetBorder(true)
	sidebar.SetSizePolicy(tui.Minimum, tui.Expanding)

	history := tui.NewVBox()
	history.SetBorder(true)
	history.SetSizePolicy(tui.Expanding, tui.Expanding)

	for _, m := range state.ChatHistory {
		b := tui.NewHBox(
			tui.NewLabel(m.Time),
			tui.NewPadder(1, 0, tui.NewLabel(fmt.Sprintf("<%s>", state.Configuration.HostName))),
			tui.NewLabel(m.Data),
		)
		history.Append(b)
	}

	input := tui.NewEntry()
	input.SetFocused(true)
	input.SetSizePolicy(tui.Expanding, tui.Minimum)

	inputBox := tui.NewHBox(input)
	inputBox.SetBorder(true)
	inputBox.SetSizePolicy(tui.Expanding, tui.Minimum)

	console := tui.NewVBox(history, inputBox)
	console.SetSizePolicy(tui.Expanding, tui.Expanding)

	input.OnSubmit(func(e *tui.Entry) {
		if len(e.Text()) > 0 {
			b := tui.NewHBox(
				tui.NewLabel(time.Now().Format("15:04")),
				tui.NewPadder(1, 0, tui.NewLabel(fmt.Sprintf("<%s>", state.Configuration.HostName))),
				tui.NewLabel(e.Text()),
			)
			history.Append(b)
			input.SetText("")
		}
	})

	root := tui.NewHBox(sidebar, console)
	root.SetSizePolicy(tui.Expanding, tui.Expanding)

	if err := tui.New(root).Run(); err != nil {
		tui.NewPadder(1, 0, tui.NewLabel(fmt.Sprintf("<Error> %v", err)))
		//	panic(err)
	}
}
