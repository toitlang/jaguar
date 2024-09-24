// Copyright (C) 2024 Florian Loitsch <florian@loitsch.com>
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"hash/crc32"
	"strings"
	"sync"

	"github.com/toitware/ubjson"
	"tinygo.org/x/bluetooth"
)

const (
	// These constants must be kept in sync with the ones in the Toit implementation.
	JaguarServiceUUID = "0cfb6d88-d865-41c4-a7a9-4986ae5cb64c"
	JaguarToken       = "Jag-\x70\x17"
	PingUUID          = "0cfbff01-d865-41c4-a7a9-4986ae5cb64c"
	IdentifyUUID      = "0cfbff02-d865-41c4-a7a9-4986ae5cb64c"
	ContainerListUUID = "0cfbff03-d865-41c4-a7a9-4986ae5cb64c"
	UninstallUUID     = "0cfbff04-d865-41c4-a7a9-4986ae5cb64c"
	StartUploadUUID   = "0cfbff05-d865-41c4-a7a9-4986ae5cb64c"
	UploadUUID        = "0cfbff06-d865-41c4-a7a9-4986ae5cb64c"

	BLEUploadKindInstall = 0
	BLEUploadKindRun     = 1

	BLEReturnCodeOK                 = 0
	BLEReturnCodeError              = 1
	BLEReturnCodeSdkVersionMismatch = 2
	BLEReturnCodeUnknownKind        = 3
)

type DeviceBle struct {
	DeviceBase
	bleDevice *bluetooth.Device
}

type SerializableDeviceBle struct {
	Kind       string `json:"kind" yaml:"kind" ubjson:"kind"`
	ID         string `json:"id" yaml:"id" ubjson:"id"`
	Name       string `json:"name" yaml:"name" ubjson:"name"`
	Chip       string `json:"chip" yaml:"chip" ubjson:"chip"`
	SDKVersion string `json:"sdkVersion" yaml:"sdkVersion" ubjson:"sdkVersion"`
	WordSize   int    `json:"wordSize" yaml:"wordSize" ubjson:"wordSize"`
	Address    string `json:"address" yaml:"address" ubjson:"address"`
}

func NewDeviceBleFromJson(data map[string]interface{}, address string) (*DeviceBle, error) {
	return &DeviceBle{
		DeviceBase: DeviceBase{
			id:         stringOr(data, "id", ""),
			name:       stringOr(data, "name", ""),
			chip:       stringOr(data, "chip", "esp32"),
			sdkVersion: stringOr(data, "sdkVersion", ""),
			wordSize:   intOr(data, "wordSize", 4),
			address:    stringOr(data, "address", address),
		},
	}, nil
}

func NewDeviceBleFromSerializable(serializable *SerializableDeviceBle) (*DeviceBle, error) {
	return &DeviceBle{
		DeviceBase: DeviceBase{
			id:         serializable.ID,
			name:       serializable.Name,
			chip:       serializable.Chip,
			sdkVersion: serializable.SDKVersion,
			wordSize:   serializable.WordSize,
			address:    serializable.Address,
		},
	}, nil
}

func (d *DeviceBle) ToSerializable() interface{} {
	return &SerializableDeviceBle{
		Kind:       "ble",
		ID:         d.ID(),
		Name:       d.Name(),
		Chip:       d.Chip(),
		SDKVersion: d.SDKVersion(),
		WordSize:   d.WordSize(),
		Address:    d.Address(),
	}
}

func withCharacteristic(address string, uuidString string, callback func(bluetooth.DeviceCharacteristic) error) error {
	return withCharacteristics(address, []string{uuidString}, func(characteristics []bluetooth.DeviceCharacteristic) error {
		return callback(characteristics[0])
	})
}

func withCharacteristics(address string, uuidStrings []string, callback func([]bluetooth.DeviceCharacteristic) error) error {
	jagUUID, err := bluetooth.ParseUUID(JaguarServiceUUID)
	if err != nil {
		return err
	}
	charUUIDs := make([]bluetooth.UUID, len(uuidStrings))
	for i, uuidString := range uuidStrings {
		charUUID, err := bluetooth.ParseUUID(uuidString)
		if err != nil {
			return err
		}
		charUUIDs[i] = charUUID
	}

	adapter, err := EnabledAdapter()
	if err != nil {
		return err
	}

	var bleAddress bluetooth.Address

	// DBUS apparently wants to have seen the device through a scan first.
	adapter.Scan(func(adapter *bluetooth.Adapter, scanResult bluetooth.ScanResult) {
		if scanResult.Address.String() == address {
			bleAddress = scanResult.Address
			adapter.StopScan()
		}
	})

	device, err := adapter.Connect(bleAddress, bluetooth.ConnectionParams{})
	if err != nil {
		return err
	}
	defer device.Disconnect()
	// Get the Jaguar service.
	services, err := device.DiscoverServices([]bluetooth.UUID{jagUUID})
	if err != nil {
		// Likely just didn't have the service.
		return err
	}
	service := services[0]
	// Connect to the device and get its identity.
	chars, err := service.DiscoverCharacteristics(charUUIDs)
	if err != nil {
		return err
	}
	return callback(chars)
}

func (d *DeviceBle) Ping(ctx context.Context, sdk *SDK) bool {
	result := false
	withCharacteristic(d.Address(), PingUUID, func(characteristic bluetooth.DeviceCharacteristic) error {
		// Write a random 4 byte sequence and read it back.
		sequence := make([]byte, 4)
		_, err := rand.Read(sequence)
		if err != nil {
			return err
		}
		_, err = characteristic.WriteWithoutResponse(sequence)
		if err != nil {
			return err
		}
		readSequence := make([]byte, 4)
		readCount, err := characteristic.Read(readSequence)
		if err != nil {
			return err
		}
		readSequence = readSequence[:readCount]
		if !bytes.Equal(sequence, readSequence) {
			return nil
		}
		result = true
		return nil
	})
	return result
}

func (d *DeviceBle) ContainerList(ctx context.Context, sdk *SDK) (map[string]string, error) {
	result := map[string]string{}
	err := withCharacteristic(d.address, ContainerListUUID, func(characteristic bluetooth.DeviceCharacteristic) error {
		// Get the number of containers by using index 0xffff.
		characteristic.WriteWithoutResponse([]byte("\xff\xff"))
		data := make([]byte, 2)
		readCount, err := characteristic.Read(data)
		if err != nil {
			return err
		}
		if readCount != 2 {
			return fmt.Errorf("expected 2 bytes, got %d", readCount)
		}
		containerCount := int(data[1])<<8 + int(data[0])

		for i := 0; i < containerCount; i++ {
			request := []byte{0, 0}
			// Little endian.
			request[0] = byte(i) & 0xff
			request[1] = byte(i>>8) & 0xff
			characteristic.WriteWithoutResponse(request)
			data := make([]byte, 1024)
			readCount, err := characteristic.Read(data)
			if err != nil {
				return err
			}
			data = data[:readCount]
			var container []string
			err = ubjson.Unmarshal(data, &container)
			if err != nil {
				return err
			}
			result[container[0]] = container[1]
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (d *DeviceBle) ContainerUninstall(ctx context.Context, sdk *SDK, name string) error {
	return withCharacteristic(d.address, UninstallUUID, func(characteristic bluetooth.DeviceCharacteristic) error {
		// Write the container id.
		_, err := characteristic.WriteWithoutResponse([]byte(name))
		return err
	})
}

func (d *DeviceBle) SendCode(ctx context.Context, sdk *SDK, request string, b []byte, headersMap map[string]string) error {
	err := withCharacteristics(d.address, []string{StartUploadUUID, UploadUUID}, func(characteristics []bluetooth.DeviceCharacteristic) error {
		startCharacteristic := characteristics[0]

		isRun := request == "/run"
		payload := []byte{}
		kind := BLEUploadKindRun
		if !isRun {
			kind = BLEUploadKindInstall
		}
		payload = append(payload, byte(kind))
		sdkVersion := sdk.Version
		payload = appendUint16Le(payload, uint16(len(sdkVersion)))
		payload = append(payload, []byte(sdkVersion)...)
		payload = appendUint32Le(payload, uint32(len(b)))
		payload = appendUint32Le(payload, crc32.ChecksumIEEE(b))
		if !isRun {
			containerName, ok := headersMap[JaguarContainerNameHeader]
			if !ok {
				return fmt.Errorf("missing container name")
			}
			payload = appendUint16Le(payload, uint16(len(containerName)))
			payload = append(payload, []byte(containerName)...)
		}
		defines := extractDefines(headersMap)
		encodedDefines, err := ubjson.Marshal(defines)
		if err != nil {
			return err
		}
		payload = append(payload, encodedDefines...)

		_, err = startCharacteristic.WriteWithoutResponse(payload)
		if err != nil {
			return err
		}

		// Read back the response to see if the device is ready to receive the data.
		response := make([]byte, 1)
		readCount, err := startCharacteristic.Read(response)
		if err != nil {
			return err
		}
		if readCount != 1 {
			return fmt.Errorf("device not ready")
		}
		if response[0] == BLEReturnCodeSdkVersionMismatch {
			return fmt.Errorf("sdk version mismatch")
		}
		if response[0] != BLEReturnCodeOK {
			return fmt.Errorf("device returned error code %d", response[0])
		}

		uploadCharacteristic := characteristics[1]
		var reader = NewProgressReader(b)
		length := len(b)
		written := 0
		for written < length {
			// We deliberately use a smaller chunk size. A chunk size of 512-3 often fails with
			// "Error: Operation failed with ATT error: 0x0e".
			// We don't use 256, as there are a few bytes needed for the header.
			chunkSize := 252
			if written+chunkSize > length {
				chunkSize = length - written
			}
			chunk := make([]byte, chunkSize)
			reader.Read(chunk)

			_, err := uploadCharacteristic.WriteWithoutResponse(chunk)
			if err != nil {
				return err
			}
			written += chunkSize
		}
		return nil
	})
	fmt.Println("") // Add a newline in case we used a progress bar.
	return err
}

func (d *DeviceBle) UpdateFirmware(ctx context.Context, sdk *SDK, b []byte) error {
	return fmt.Errorf("Firmware update over BLE is unsupported")
}

func isJaguarDevice(scanResult bluetooth.ScanResult) bool {
	manufacturerData := scanResult.ManufacturerData()
	for _, entry := range manufacturerData {
		dataBytes := entry.Data
		if string(dataBytes) != JaguarToken {
			continue
		}
		return true
	}
	return false
}

func readIdentity(address string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := withCharacteristic(address, IdentifyUUID, func(characteristic bluetooth.DeviceCharacteristic) error {
		// Read the identity.
		data := make([]byte, 1024)
		readCount, err := characteristic.Read(data)
		if err != nil {
			return err
		}
		data = data[:readCount]
		// Parse the identity as JSON.
		err = ubjson.Unmarshal(data, &result)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func ScanBle(ctx context.Context, ds deviceSelect) ([]Device, error) {
	adapter, err := EnabledAdapter()
	if isBLENotExist(err) {
		return []Device{}, nil
	}
	if err != nil {
		return nil, err
	}

	// Start a goroutine to linet for context cancellation.
	go func() {
		select {
		case <-ctx.Done():
			// When the context is cancelled, stop scanning.
			adapter.StopScan()
		}
	}()

	var wg sync.WaitGroup
	devices := []Device{}

	found := map[string]bool{}
	err = adapter.Scan(func(adapter *bluetooth.Adapter, scanResult bluetooth.ScanResult) {
		address := scanResult.Address.String()
		// Add the device to the found map.
		// Overwrite the device if it was already found.
		if _, ok := found[address]; ok {
			return
		}
		found[address] = true
		if isJaguarDevice(scanResult) {
			wg.Add(1)
			go func(scanResult bluetooth.ScanResult) {
				defer wg.Done()
				identity, err := readIdentity(address)
				if err != nil {
					fmt.Printf("Couldn't read identity of '%s': %s", scanResult.LocalName(), err)
					return
				}
				jaguarDev, err := NewDeviceBleFromJson(identity, address)
				if err != nil {
					return
				}
				devices = append(devices, jaguarDev)
			}(scanResult)
		}
	})
	if err != nil {
		return nil, err
	}
	wg.Wait()

	return devices, nil
}

func isBLENotExist(err error) bool {
	return err != nil && strings.Contains(err.Error(), "does not exist")
}
