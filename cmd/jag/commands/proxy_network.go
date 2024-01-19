// Copyright (C) 2024 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
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
)

func runProxyServer(ud *uartDevice, identity *uartIdentity) error {
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

	readBody := func(body io.Reader, length int64) ([]byte, error) {
		result := make([]byte, length)
		_, err := io.ReadFull(body, result)
		if err != nil {
			return nil, err
		}
		return result, nil
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
		firmwareImage, err := readBody(r.Body, r.ContentLength)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		err = ud.Firmware(firmwareImage)
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
		containerImage, err := readBody(r.Body, r.ContentLength)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		err = ud.Install(containerName, defines, containerImage)
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
		image, err := readBody(r.Body, r.ContentLength)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		err = ud.Run(defines, image)
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
	fmt.Printf("Proxying device %s through 'http://%s:%d'.\n", identity.Name, localIP, localPort)
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
			"name":       identity.Name,
			"id":         identity.Id,
			"chip":       identity.Chip,
			"sdkVersion": identity.SdkVersion,
			"address":    "http://" + localIP + ":" + strconv.Itoa(localPort),
			"wordSize":   4,
			"proxied":    true,
		},
	}
	return json.Marshal(jsonIdentity)
}

func broadcastIdentity(identityPayload []byte) error {
	// Create a goroutine to send the payload every 200ms.
	go func() {
		for {
			// Create a UDP address for broadcasting (use broadcast IP)
			addr, err := net.ResolveUDPAddr("udp", udpIdentifyAddress+":"+strconv.Itoa(udpIdentifyPort))
			if err != nil {
				// Sleep for a few seconds and try again.
				time.Sleep(5 * time.Second)
				continue
			}

			// Create a UDP connection
			conn, err := net.DialUDP("udp", nil, addr)
			if err != nil {
				// Sleep for a few seconds and try again.
				time.Sleep(5 * time.Second)
				continue
			}

			ticker := time.NewTicker(200 * time.Millisecond)

			for range ticker.C {
				_, err := conn.Write(identityPayload)
				if err != nil {
					println("Error broadcasting payload:", err.Error())
					ticker.Stop()
					conn.Close()
					// Try to reconnect.
					break
				}
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
