// Copyright (C) 2024 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/toitware/ubjson"
)

// A small HTTP server that can be used to communicate with the device through
// the UART.

const (
	// The URL for the favicon.
	faviconURL = "https://toitlang.github.io/jaguar/device-files/chip.svg"
	// The URL for the chip image.
	chipURL = "https://toitlang.github.io/jaguar/device-files/chip.svg"
	// The URL for the style sheet.
	styleURL = "https://toitlang.github.io/jaguar/device-files/style.css"

	headerDeviceId         = "X-Jaguar-Device-ID"
	headerSdkVersion       = "X-Jaguar-SDK-Version"
	headerDisabled         = "X-Jaguar-Disabled"
	headerContainerName    = "X-Jaguar-Container-Name"
	headerContainerTimeout = "X-Jaguar-Container-Timeout"

	defineJagDisabled = "jag.disabled"
	defineJagTimeout  = "jag.timeout"

	udpIdentifyPort = 1990
	// Broadcast.
	udpIdentifyAddress = "255.255.255.255"

	// Commands.
	commandSync           = 0
	commandPing           = 1
	commandIdentify       = 2
	commandListContainers = 3
	commandUninstall      = 4
	commandFirmware       = 5
	commandInstall        = 6
	commandRun            = 7

	responseAck = 255
)

// A sequence of random numbers that is used as synchronization token.
var syncMagic = []byte{27, 121, 55, 49, 253, 65, 123, 243}

func uartName(name string) string {
	return name + "-uart"
}

func serveSerial(dev *serialPort, reader io.Reader) error {
	ud := newUartDevice(dev, reader)

	err := ud.Sync()
	if err != nil {
		return err
	}

	identity, err := ud.Identify()
	if err != nil {
		// TODO(florian): this print should be a log.
		fmt.Println("Identify error")
		return err
	}

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return err
	}
	defer listener.Close()

	// Get the local IP and the dynamically assigned port.
	localAddr := listener.Addr().(*net.TCPAddr)
	localIP, err := getLanIp()
	if err != nil {
		return err
	}
	localPort := localAddr.Port

	identityPayload, err := createIdentityPayload(identity, localIP, localPort)
	if err != nil {
		return err
	}

	checkValidDeviceId := func(w http.ResponseWriter, r *http.Request) bool {
		deviceId := r.Header.Get(headerDeviceId)
		if deviceId != "" && deviceId != identity.Id {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("Device has id '" + identity.Id + "', jag is trying to talk to '" + deviceId + "'.\n"))
			// TODO(florian): this print should be a log.
			fmt.Println("Denied request: Header '" + headerDeviceId + "' has value '" + deviceId + "' but expected '" + identity.Id + "'.")
			return false
		}
		return true
	}

	checkSameSDK := func(w http.ResponseWriter, r *http.Request) bool {
		sdkVersion := r.Header.Get(headerSdkVersion)
		if sdkVersion != "" && sdkVersion != identity.SdkVersion {
			w.WriteHeader(http.StatusNotAcceptable)
			w.Write([]byte("Device has SDK version '" + identity.SdkVersion + "', jag has '" + sdkVersion + "'.\n"))
			// TODO(florian): this print should be a log.
			fmt.Println("Denied request: Header '" + headerSdkVersion + "' has value '" + sdkVersion + "' but expected '" + identity.SdkVersion + "'.")
			return false
		}
		return true
	}

	checkIsPut := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return false
		}
		return true
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/identify", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "application/json")
		w.Write(identityPayload)
	})
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		if !checkValidDeviceId(w, r) {
			return
		}
		err := ud.Ping()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/list", func(w http.ResponseWriter, r *http.Request) {
		if !checkValidDeviceId(w, r) {
			return
		}
		encodedContainers, err := ud.ListContainers()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Add("Content-Type", "application/ubjson")
		w.Header().Add("Content-Length", strconv.Itoa(len(encodedContainers)))
		w.Write(encodedContainers)
	})
	mux.HandleFunc("/uninstall", func(w http.ResponseWriter, r *http.Request) {
		if !checkValidDeviceId(w, r) || !checkIsPut(w, r) {
			return
		}
		containerName := r.Header.Get(headerContainerName)
		if containerName == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		err := ud.Uninstall(containerName)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/firmware", func(w http.ResponseWriter, r *http.Request) {
		if !checkValidDeviceId(w, r) || !checkIsPut(w, r) {
			return
		}
		err := ud.Firmware(r.ContentLength, r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/install", func(w http.ResponseWriter, r *http.Request) {
		if !checkValidDeviceId(w, r) || !checkSameSDK(w, r) || !checkIsPut(w, r) {
			return
		}
		containerName := r.Header.Get(headerContainerName)
		if containerName == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defines := extractDefines(r)
		err := ud.Install(containerName, defines, r.ContentLength, r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/run", func(w http.ResponseWriter, r *http.Request) {
		if !checkValidDeviceId(w, r) || !checkSameSDK(w, r) || !checkIsPut(w, r) {
			return
		}
		defines := extractDefines(r)
		err := ud.Run(defines, r.ContentLength, r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Catchall handler.
		if strings.HasSuffix(r.URL.Path, ".html") ||
			strings.HasSuffix(r.URL.Path, ".css") ||
			strings.HasSuffix(r.URL.Path, ".ico") {
			serveBrowser(identity, w, r)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	})

	// TODO(florian): this print should be a log.
	fmt.Printf("Proxying device through 'http://%s:%d'.\n", localIP, localPort)
	err = broadcastIdentity(identityPayload)
	if err != nil {
		return err
	}
	return http.Serve(listener, mux)
}

func createIdentityPayload(identity *uartIdentity, localIP string, localPort int) ([]byte, error) {
	jsonIdentity := map[string]interface{}{
		"method": "jaguar.identify",
		"payload": map[string]interface{}{
			"name":       identity.Name + "-uart",
			"id":         identity.Id,
			"chip":       identity.Chip,
			"sdkVersion": identity.SdkVersion,
			"address":    "http://" + localIP + ":" + strconv.Itoa(localPort),
			"wordSize":   4,
		},
	}
	return json.Marshal(jsonIdentity)
}

func broadcastIdentity(identityPayload []byte) error {
	// Create a UDP address for broadcasting (use broadcast IP)
	addr, err := net.ResolveUDPAddr("udp", udpIdentifyAddress+":"+strconv.Itoa(udpIdentifyPort))
	if err != nil {
		return err
	}

	// Create a UDP connection
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return err
	}

	// Create a goroutine to send the payload every 200ms
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {
			_, err := conn.Write(identityPayload)
			if err != nil {
				// Handle error, e.g., log it
				// Note: You might want to add more error handling based on your specific use case
				println("Error broadcasting payload:", err.Error())
			}
		}
	}()
	return nil
}

func extractDefines(r *http.Request) map[string]interface{} {
	defines := map[string]interface{}{}
	if r.Header.Get(headerDisabled) != "" {
		defines[defineJagDisabled] = true
	}
	if r.Header.Get(headerContainerTimeout) != "" {
		val := r.Header.Get(headerContainerTimeout)
		// Parse the integer value.
		if timeout, err := strconv.Atoi(val); err == nil {
			defines[defineJagTimeout] = timeout
		}
	}
	return defines
}

func serveBrowser(identity *uartIdentity, w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" || r.URL.Path == "/index.html" {
		w.Header().Add("Content-Type", "text/html")
		w.Write([]byte(`
<html>
<head>
	<link rel="stylesheet" href="` + styleURL + `">
	<title>` + identity.Name + ` (Jaguar device)</title>
</head>
<body>
	<div class="box">
		<section class="text-center">
			<img src="` + chipURL + `" alt="Picture of an embedded device" width=200>
		</section>
		<h1 class="mt-40">` + uartName(identity.Name) + `</h1>
		<p class="text-center">Jaguar device</p>
		<p class="mt-40">Device proxied through 'jag monitor'.</p>
		<p class="hr mt-40"></p>
		<section class="grid grid-cols-2 mt-20">
			<p>SDK</p>
			<p><b class="text-black">` + identity.SdkVersion + `</b></p>
		</section>
		<p class="hr mt-20"></p>
		<p class="mt-40">Run code on this device using</p>
		<b><a href="https://github.com/toitlang/jaguar">&gt; jag run -d ` + uartName(identity.Name) + ` hello.toit</a></b>
	</div>
</body>
</html>
`))
	} else if r.URL.Path == "/favicon.ico" {
		// Redirect to the default favicon.
		http.Redirect(w, r, faviconURL, http.StatusFound)
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
}

type uartDevice struct {
	lock   sync.Mutex
	writer io.Writer
	reader *bufio.Reader
	syncId int
}

func newUartDevice(writer io.Writer, reader io.Reader) *uartDevice {
	return &uartDevice{
		writer: writer,
		reader: bufio.NewReader(reader),
	}
}

// Sync synchronizes the device with the server.
// The server repeatedly sends a sync request to the device, and the device
// responds with a sync response.
func (d *uartDevice) Sync() error {
	d.lock.Lock()
	defer d.lock.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	errc := make(chan error)
	successc := make(chan struct{})
	stopc := make(chan struct{})
	done := false

	go func() {
		for !done {
			// Use the '\n' of messages to align the reader.
			data, err := d.reader.ReadBytes('\n')
			if err != nil {
				errc <- err
				return
			}
			if len(data) == 0 {
				continue
			}
			expectedSyncPacket := buildExpectedSyncResponse(d.syncId)
			if !bytes.Equal(data, expectedSyncPacket) {
				// Discard the data.
				continue
			}
			successc <- struct{}{}
			return
		}
	}()

	go func() {
		for {
			select {
			case <-ctx.Done():
				errc <- ctx.Err()
				return
			case <-stopc:
				return
			default:
				// Send a sync request.
				// Increment before building, so that the read function can use the id without adjustments.
				d.syncId++
				syncRequest := buildSyncRequest(d.syncId)
				err := d.writeAll(syncRequest)
				if err != nil {
					errc <- err
					return
				}
				time.Sleep(2 * time.Second)
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			done = true
			stopc <- struct{}{}
			return ctx.Err()
		case err := <-errc:
			done = true
			stopc <- struct{}{}
			return err
		case <-successc:
			done = true
			stopc <- struct{}{}
			return nil
		}
	}
}

func (d *uartDevice) Ping() error {
	d.lock.Lock()
	defer d.lock.Unlock()
	response, err := d.sendRequest(commandPing, []byte{})
	if err != nil {
		return err
	}
	if len(response) != 0 {
		return fmt.Errorf("invalid ping response")
	}
	return nil
}

type uartIdentity struct {
	Name       string `json:"name"`
	Id         string `json:"id"`
	Chip       string `json:"chip"`
	SdkVersion string `json:"sdkVersion"`
}

func (d *uartDevice) Identify() (*uartIdentity, error) {
	d.lock.Lock()
	defer d.lock.Unlock()
	response, err := d.sendRequest(commandIdentify, []byte{})
	if err != nil {
		return nil, err
	}
	var identity uartIdentity
	err = ubjson.Unmarshal(response, &identity)
	if err != nil {
		return nil, err
	}
	return &identity, nil
}

func (d *uartDevice) ListContainers() ([]byte, error) {
	d.lock.Lock()
	defer d.lock.Unlock()
	return d.sendRequest(commandListContainers, []byte{})
}

func (d *uartDevice) Uninstall(containerName string) error {
	d.lock.Lock()
	defer d.lock.Unlock()
	_, err := d.sendRequest(commandUninstall, []byte(containerName))
	return err
}

func (d *uartDevice) Firmware(length int64, newFirmware io.Reader) error {
	d.lock.Lock()
	defer d.lock.Unlock()
	payload := []byte{}
	payload = appendUint32Le(payload, uint32(length))
	_, err := d.sendRequest(commandFirmware, payload)
	if err != nil {
		return err
	}
	return d.streamChunked(length, newFirmware)
}

func (d *uartDevice) Install(containerName string, defines map[string]interface{}, length int64, imageReader io.Reader) error {
	d.lock.Lock()
	defer d.lock.Unlock()
	encodedDefines, err := ubjson.Marshal(defines)
	if err != nil {
		return err
	}
	payload := []byte{}
	payload = appendUint32Le(payload, uint32(length))
	payload = appendUint16Le(payload, uint16(len(containerName)))
	payload = append(payload, containerName...)
	payload = append(payload, encodedDefines...)

	_, err = d.sendRequest(commandInstall, payload)
	if err != nil {
		return err
	}
	return d.streamChunked(length, imageReader)
}

func (d *uartDevice) Run(defines map[string]interface{}, length int64, imageReader io.Reader) error {
	d.lock.Lock()
	defer d.lock.Unlock()
	encodedDefines, err := ubjson.Marshal(defines)
	if err != nil {
		return err
	}
	payload := []byte{}
	payload = appendUint32Le(payload, uint32(length))
	payload = append(payload, encodedDefines...)

	_, err = d.sendRequest(commandRun, payload)
	if err != nil {
		return err
	}
	return d.streamChunked(length, imageReader)
}

func appendUint16Le(data []byte, value uint16) []byte {
	data = append(data, byte(value&0xff))
	data = append(data, byte(value>>8))
	return data
}

func appendUint32Le(data []byte, value uint32) []byte {
	data = append(data, byte(value&0xff))
	data = append(data, byte(value>>8))
	data = append(data, byte(value>>16))
	data = append(data, byte(value>>24))
	return data
}

func (d *uartDevice) streamChunked(length int64, dataReader io.Reader) error {
	// Start sending the image in chunks of 512 bytes.
	// Expect a response for each chunk.
	written := 0
	for written < int(length) {
		chunkSize := 512
		if written+chunkSize > int(length) {
			chunkSize = int(length) - written
		}
		chunk := make([]byte, chunkSize)
		_, err := io.ReadFull(dataReader, chunk)
		if err != nil {
			return err
		}
		d.writeAll(chunk)
		written += chunkSize
		consumed := 0
		for consumed < chunkSize {
			// Read the Ack. 3 bytes.
			buffer := make([]byte, 3)
			_, err = io.ReadFull(d.reader, buffer)
			if err != nil {
				return err
			}
			if buffer[0] != responseAck {
				return fmt.Errorf("invalid ack")
			}
			consumed += int(buffer[1]) | (int(buffer[2]) << 8)
			if consumed > chunkSize {
				return fmt.Errorf("invalid ack")
			}
		}
	}
	return nil
}

func (d *uartDevice) sendRequest(command byte, payload []byte) ([]byte, error) {
	err := d.WritePacket(append([]byte{command}, payload...))
	if err != nil {
		return nil, err
	}
	return d.ReceiveResponse(command)
}

// WritePacket writes the given data as a packet.
// Packets start with their payload-length, followed by the payload, and end
// with a zero byte.
func (d *uartDevice) WritePacket(data []byte) error {
	if len(data) > 65535 {
		return io.ErrShortBuffer
	}
	length := len(data)
	err := d.writeAll([]byte{byte(length & 0xff), byte(length >> 8)})
	if err != nil {
		return err
	}
	err = d.writeAll(data)
	if err != nil {
		return err
	}
	return d.writeAll([]byte{'\n'})
}

func (d *uartDevice) writeAll(data []byte) error {
	for len(data) > 0 {
		count, err := d.writer.Write(data)
		if err != nil {
			return err
		}
		data = data[count:]
	}
	return nil
}

// ReceiveResponse receives a response to the given command.
// The returned data is the payload of the response, without the command byte.
func (d *uartDevice) ReceiveResponse(command byte) ([]byte, error) {
	lengthLsb, err := d.reader.ReadByte()
	if err != nil {
		return nil, err
	}
	lengthMsb, err := d.reader.ReadByte()
	if err != nil {
		return nil, err
	}
	payloadLength := int(lengthLsb) | (int(lengthMsb) << 8)
	payload := make([]byte, payloadLength)
	_, err = io.ReadFull(d.reader, payload)
	if err != nil {
		return nil, err
	}
	// Make sure the packet ends with a zero byte.
	b, err := d.reader.ReadByte()
	if err != nil {
		return nil, err
	}
	if b != '\n' {
		return nil, fmt.Errorf("invalid packet terminator")
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("empty payload")
	}
	if payload[0] != command {
		return nil, fmt.Errorf("unmatched response")
	}
	return payload[1:], nil
}

func buildRequest(command byte, data []byte) []byte {
	payload := []byte{}
	payload = append(payload, command)
	payload = append(payload, data...)
	payloadSize := len(payload)
	if payloadSize > 65535 {
		panic("payload too large")
	}
	request := []byte{}
	request = appendUint16Le(request, uint16(payloadSize))
	request = append(request, payload...)
	request = append(request, '\n')
	return request
}

func buildSyncRequest(syncId int) []byte {
	data := []byte{}
	data = appendUint16Le(data, uint16(syncId))
	for i := 0; i < len(syncMagic); i++ {
		// The magic number is sent with each byte decremented by one to avoid
		// accidental detections.
		data = append(data, syncMagic[i]-1)
	}
	return buildRequest(commandSync, data)
}

func buildExpectedSyncResponse(syncId int) []byte {
	response := []byte{}
	payload := []byte{}
	payload = append(payload, commandSync)
	payload = appendUint16Le(payload, uint16(syncId))

	response = appendUint16Le(response, uint16(len(payload)))
	response = append(response, payload...)
	response = append(response, '\n')
	return response
}
