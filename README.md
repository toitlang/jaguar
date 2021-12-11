
# Jaguar

Develop, update, and restart your ESP32 applications in less than two seconds.

![shaguar](https://user-images.githubusercontent.com/22043/145008669-65d31451-99fc-4965-b087-2ac48ce5ac53.jpeg)

## What is it?

Jaguar is a small Toit program that runs on your ESP32. It uses the capabilities of the
[Toit virtual machine](https://github.com/toitlang/toit) to let you update and restart your ESP32
applications written in Toit over WiFi. Change your Toit code in your editor, update the application on
your device, and restart it all within seconds. No need to flash over serial, reboot your device, or wait
for it to reconnect to your network.

## How does it work?

Jaguar runs a small HTTP server that listens for incoming requests. The requests contain compiled
Toit applications that are relocated and installed in flash on the device. Before installing an
application, we stop any old version of the application and free the resources it has consumed. The new
version of the application gets to start again from `main`.

## How do I use it?

First you'll need to have a [Go development environment](https://go.dev) properly set up. Using that
you can install the `jag` command line tool using `go install`:

``` sh
go install github.com/toitlang/jaguar/cmd/jag@latest
```

Remember to add `$HOME/go/bin` or `%USERPROFILE%\go\bin` to your `PATH`. Next step is to let `jag` 
download and configure the Toit SDK and the associated tools for flashing the Jaguar application 
onto your ESP32:

``` sh
jag setup
```

Now it is time to connect your ESP32 with a serial cable to your computer and put the Jaguar
application onto it:

``` sh
jag flash --port=/dev/ttyUSB0 --wifi-ssid="<ssid>" --wifi-password="<password>"
```

Now it is possible to monitor the serial output from the device:

``` sh
jag monitor --port=/dev/ttyUSB0
```

Once the serial output shows that your ESP32 runs the Jaguar application, it will start announcing
its presence to the network using UDP broadcast. You can find a device by scanning, but this requires
you to be on the same local network as your ESP32:

``` sh
jag scan
```

With the scanning complete, you're ready to run your first Toit program on your Jaguar-enabled
ESP32 device:

``` sh
jag run examples/hello.toit
```

# Building it yourself

You've read this far and you want to know how to build Jaguar and the underlying Toit language
implementation yourself? Great! You will need to follow the instructions for
[building Toit](https://github.com/toitlang/toit) and make sure you can flash a
[simple example](https://github.com/toitlang/toit/blob/master/examples/hello.toit) onto your device.

Let's assume your git clones can be referenced like this:

``` sh
export TOIT_PATH=<path to https://github.com/toitlang/toit clone>
export JAGUAR_PATH=<path to https://github.com/toitlang/jaguar clone>
```

Now start with flashing the Jaguar application onto your ESP32. This is easily doable from
within the `$TOIT_PATH` directory:

``` sh
make flash ESP32_ENTRY=$JAGUAR_PATH/src/jaguar.toit \
  ESP32_PORT=/dev/ttyUSB0 \
  ESP32_WIFI_SSID="<ssid>" \
  ESP32_WIFI_PASSWORD="<password>"
```

For building Jaguar, all you need to do is run from within your `$JAGUAR_PATH` directory:

``` sh
make
```

You can now run Jaguar by telling it where to find the Toit SDK and the snapshot for the
Jaguar application for the ESP32:

``` sh
export JAG_TOIT_PATH=$TOIT_PATH/build/host/sdk
export JAG_ENTRY_POINT=$TOIT_PATH/build/snapshot
build/jag scan
```

To also do the ESP32 flashing through Jaguar, you'll need two extra environment variables:

``` sh
export JAG_ESP32_BIN_PATH=$TOIT_PATH/build/esp32
export JAG_ESPTOOL_PATH=$IDF_PATH/components/esptool_py/esptool/esptool.py
build/jag flash --port=/dev/ttyUSB0 --wifi-ssid="<ssid>" --wifi-password="<password>"
```

## Contributing

We welcome and value your [open source contributions](CONTRIBUTING.md) to Jaguar (or Shaguar as we
like to call it).
