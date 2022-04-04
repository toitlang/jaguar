# Jaguar: Live reloading for your ESP32

Jaguar enables live reloading when developing for the ESP32. Develop, update, and restart
your code in less than two seconds via WiFi. Use the really fast development cycle to iterate
quickly and learn fast!

## What is it?

Jaguar is a small Toit application that runs on your ESP32. It uses the capabilities of the
[Toit virtual machine](https://github.com/toitlang/toit) to let you update and restart your ESP32
code written in Toit over WiFi. Change your code in your editor, update it on
your device, and restart it all within seconds. No need to flash over serial, reboot your device, or wait
for it to reconnect to your network.

Watch a short video that shows how you can experience Jaguar on your ESP32 in two minutes:

<a href="https://youtu.be/cU7zr6_YBbQ"><img width="543" alt="Jaguar demonstration" src="https://user-images.githubusercontent.com/133277/146210503-24811800-bb26-4244-817d-6422b20e6786.png"></a>

## How does it work?

Jaguar runs a small HTTP server that listens for incoming requests. The requests contain compiled
Toit programs that are relocated and installed in flash on the device. Before installing a
program, we stop any old version of the program and free the resources it has consumed. The new
version of the program gets to start again from `main`.

## How do I use it?

Unless you want to [build Jaguar from source](#building-it-yourself), start by
downloading and installing the `jag` binary for your host platform:

- [Download Jaguar for Windows](https://github.com/toitlang/jaguar/releases/latest/download/jag_installer.exe)
  (or as an [archive](https://github.com/toitlang/jaguar/releases/latest/download/jag_windows.zip))
- [Download Jaguar for macOS](https://github.com/toitlang/jaguar/releases/latest/download/jag.dmg)
  (or as an [archive](https://github.com/toitlang/jaguar/releases/latest/download/jag_macos.zip))
- [Download Jaguar for Linux](https://github.com/toitlang/jaguar/releases/latest/download/jag_linux.tgz)
  (only as an archive)

On macOS, you can also use [Homebrew](https://brew.sh/) to mange the installation of `jag`:

``` sh
brew install toitlang/toit/jag
```

If you download an archive, you should unpack it and put the embedded `jag` or `jag.exe` binary
somewhere on your `PATH`. The same applies when you extract the `jag` binary from the macOS `jag.dmg` file.

Next step is to let `jag` download and configure the Toit SDK and the associated tools for
flashing the Jaguar application onto your ESP32:

``` sh
jag setup
```

Now it is time to connect your ESP32 with a serial cable to your computer and put the Jaguar
application onto it. This will ask you for the serial port to use and the WiFi credentials:

``` sh
jag flash
```

---
*NOTE*

To flash you will need to access the device `/dev/ttyUSB0`.  On Linux that
means you probably need to be a member of some group, normally either `uucp` or
`dialout`.  To see which groups you are a member of and which group owns the
device, plug in an ESP32 to the USB port and try:

``` sh
groups
ls -g /dev/ttyUSB0
```

If you lack a group membership, you can add it with

``` sh
sudo usermod -aG dialout $USER
```

You will have to log out and log back in for this to take effect.

---

Now it is possible to monitor the serial output from the device:

``` sh
jag monitor
```

Once the serial output shows that your ESP32 runs the Jaguar application, it will start announcing
its presence to the network using UDP broadcast. You can find a device by scanning, but this requires
you to be on the same local network as your ESP32:

``` sh
jag scan
```

With the scanning complete, you're ready to run your first Toit program on your Jaguar-enabled
ESP32 device. Download [`hello.toit`](https://github.com/toitlang/toit/blob/master/examples/hello.toit)
and store it in your file system and then run:

``` sh
jag run hello.toit
```

It is even possible to ask Jaguar to keep watching your Toit code on disk and to *live reload* it when
it changes. Simply write:

``` sh
jag watch hello.toit
```

and edit `hello.toit` or any of the files it depends on in your favorite editor.

# Visual Studio Code

The Toit SDK used by Jaguar comes with support for [Visual Studio Code](https://code.visualstudio.com/download).
Once installed, you can add the [Toit language extension](https://marketplace.visualstudio.com/items?itemName=toit.toit)
and get full language support for Toit, including syntax highlighting, integrated static analysis, and code completions.
Jaguar already comes with everything you need, so if you can run `jag` from your `PATH`, the extension will automatically
find the Toit SDK downloaded by Jaguar and use that.

# Crash reporting

If you have not opted-out of Jaguar's crash reporting and usage analytics, the `jag` binary will gather
crash reports and usage statistics and forward them to [Segment](https://segment.com/). We use the statistics
to improve Jaguar and the gathered data may include:

- Basic information: The version of Jaguar and the name and version of your host operating system.
- Command line usage: Data on which commands were run, not including the actual command lines.
- Stack traces: The stack trace generated by a crash, which only contains references to `jag`'s own code.
- Anonymous ID: A constant and unique number generated for the host where Jaguar is installed.

The gathering of analytics is controlled by `$HOME/.cache/jaguar/config.yaml` or `%USERPROFILE%\.cache\jaguar\config.yaml` and you can opt
out of the data gathering by running:

``` sh
jag config analytics disable
```

If you opt out of analytics, an opt-out event will be sent, and then no further information will be sent by that installation of Jaguar.
The crash reporting component is [work in progress](https://github.com/toitlang/jaguar/issues/75).

---

# Installing via Go

You can also install using `go install`. First you'll need to have a [Go development environment](https://go.dev)
properly set up (1.16+) and remember to add `$HOME/go/bin` or `%USERPROFILE%\go\bin` to your `PATH`. Using that
you can install the `jag` command line tool through:

``` sh
go install github.com/toitlang/jaguar/cmd/jag@latest
```

# Building it yourself

You've read this far and you want to know how to build Jaguar and the underlying Toit language
implementation yourself? Great! You will need to follow the instructions for
[building Toit](https://github.com/toitlang/toit) and make sure you can flash a
[simple example](https://github.com/toitlang/toit/blob/master/examples/hello.toit) onto your device.

Let's assume your git clones can be referenced like this:

``` sh
export TOIT_PATH=<path to https://github.com/toitlang/toit clone>
export JAG_PATH=<path to https://github.com/toitlang/jaguar clone>
```

First, we need to build the `toit.pkg` support from the `$TOIT_PATH` directory:

``` sh
cd $TOIT_PATH
make sdk
```

Now we can compile the Jaguar assets necessary for flashing Jaguar onto your
device. This is easily doable from within the `$JAG_PATH` directory.

``` sh
cd $JAG_PATH
$TOIT_PATH/build/host/sdk/bin/toit.pkg install --project-root=$JAG_PATH
source $TOIT_PATH/third_party/esp-idf/export.sh
make
```

You can now flash Jaguar onto your device by telling it where to find the Toit SDK
and the pre-built image for the Jaguar application for the ESP32:

``` sh
cd $JAG_PATH
export JAG_TOIT_PATH=$TOIT_PATH/build/host/sdk
export JAG_ESP32_IMAGE_PATH=$JAG_PATH/build/image
export JAG_ESPTOOL_PATH=$IDF_PATH/components/esptool_py/esptool/esptool.py
build/jag flash --port=/dev/ttyUSB0 --wifi-ssid="<ssid>" --wifi-password="<password>"
```

The Jaguar command-line tool in `build/jag` now uses the environment variables from above
to find the Toit SDK, so you start using it:

``` sh
cd $JAG_PATH
build/jag scan
```

## Contributing

We welcome and value your [open source contributions](CONTRIBUTING.md) to Jaguar (or Shaguar as we
like to call it).

![Shaguar!](https://user-images.githubusercontent.com/22043/145008669-65d31451-99fc-4965-b087-2ac48ce5ac53.jpeg)

