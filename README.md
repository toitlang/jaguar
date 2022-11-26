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

### Download
Unless you want to [build Jaguar from source](#building-it-yourself), start by
downloading and installing the `jag` binary for your host platform.

On macOS, you can use [Homebrew](https://brew.sh/) to manage the installation of `jag`:
``` sh
brew install toitlang/toit/jag
```

On Windows 10+ you can use the [Windows package manager](https://docs.microsoft.com/en-us/windows/package-manager/winget/):
```
winget install --id=Toit.Jaguar -e
```

For Archlinux you can install the AUR package [jaguar-bin](https://aur.archlinux.org/packages/jaguar-bin):
``` sh
yay install jaguar-bin
```

As alternative to these package managers, we also offer precompiled binaries for download:

- [Download Jaguar for macOS](https://github.com/toitlang/jaguar/releases/latest/download/jag.dmg)
  (or as an [archive](https://github.com/toitlang/jaguar/releases/latest/download/jag_macos.zip))
- [Download Jaguar for Windows](https://github.com/toitlang/jaguar/releases/latest/download/jag_installer.exe)
  (or as an [archive](https://github.com/toitlang/jaguar/releases/latest/download/jag_windows.zip))
- [Download Jaguar for Linux](https://github.com/toitlang/jaguar/releases/latest/download/jag_linux.tgz)
  (only as an archive)

If you download an archive, you should unpack it and put the embedded `jag` or `jag.exe` binary
somewhere on your `PATH`. The same applies when you extract the `jag` binary from the macOS `jag.dmg` file.

### Setup associated tools
Next step is to let `jag` download and configure the Toit SDK and the associated tools for
flashing the Jaguar application onto your ESP32:

``` sh
jag setup
```

### Flashing via serial
Now it is time to connect your ESP32 with a serial cable to your computer and put the Jaguar
application onto it. Running `jag flash` will ask you for the serial port to use and the WiFi
credentials, but be aware that the tooling requires
[permission to access your serial port](#permission-to-access-serial-port).

``` sh
jag flash
```

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

### Running code via WiFi
With the scanning complete, you're ready to run your first Toit program on your Jaguar-enabled
ESP32 device. Download [`hello.toit`](https://github.com/toitlang/toit/blob/master/examples/hello.toit)
and store it in your file system and then run:

``` sh
jag run hello.toit
```

Be aware that you can configure the way your applications run by [providing options](#options-for-jag-run)
to `jag run`. Also, Jaguar is fast enough that it is possible to ask Jaguar to keep watching your Toit code
on disk and to *live reload* it when it changes. Simply write:

``` sh
jag watch hello.toit
```

and edit `hello.toit` or any of the files it depends on in your favorite editor.

### Installing services and drivers
Jaguar supports installing named containers that are automatically run when the system boots. They can be used
to provide services and implement drivers for peripherals. The services and drivers can be used by
applications and as such they form an instrumental part of the extensibility of a Jaguar based system.

You can list the currently installed containers on a device through:

``` sh
jag container list
```

This results in a list that shows the container image ids and the associated names.

```
$ jag container list
85c64060-ffbd-5e04-a0dd-252d5bbf4a32: print-service
4e9a12bc-7f07-5118-9f04-8ad2bbe476d1: jaguar
```

You install a new, or update an existing, container through:

``` sh
jag container install print-service service.toit
```

and you can uninstall said container again using:

``` sh
jag container uninstall print-service
```

### Updating Jaguar via WiFi
If you upgrade Jaguar, you will need to update the system software and the Jaguar application on your
device. You can do this via WiFi simply by invoking:

``` sh
jag firmware update
```

Updating the firmware will uninstall all containers and stop running applications, so those have to
be transfered to the device again after the update.

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

# Options for `jag run`
It is possible to provide options for `jag run` that control how your applications behave on your device. This section
lists the options and provides an explanation for when they might come in handy.

## Limiting application run time
You can control how much time Jaguar gives your application to run through the `-D jag.timeout` setting. It takes a value
like `10s`, `5m`, or `1h` to indicate how many seconds, minutes, or hours the app can run before being shut down by Jaguar.

``` sh
jag run -D jag.timeout=10s service.toit
```

## Temporarily disabling Jaguar
You can disable Jaguar while your application runs using the `-D jag.disabled`. This is useful if Jaguar otherwise
interferes with your application. As an example, consider an application that uses the WiFi to setup a
software-enabled access point ("Soft AP"). This would normally conflict with Jaguar's use of the WiFi, so your
application and Jaguar cannot run at the same time. By temporarily disabling Jaguar, it is possible to test and tinker with
the Soft AP based service.

``` sh
jag run -D jag.disabled softap.toit
```

By default this runs with a 10 seconds timeout to avoid completely shutting down Jaguar. However, this can be configured
by passing a separate `-D jag.timeout` option:

``` sh
jag run -D jag.disabled -D jag.timeout=5m softap.toit
```

This also works for installed containers. Containers that run with `-D jag.disabled` start when the device boots and
runs to completion before Jaguar is enabled. This allows them to control the WiFi and to prevent Jaguar from taking
over before they are ready for it:

``` sh
jag container install -D jag.disabled softap softap.toit
```

You can also set the timeout for them to make sure they cannot block enabling Jaguar forever:

``` sh
jag container install -D jag.disabled -D jag.timeout=20s softap softap.toit
```

---

# Permission to access serial port
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

You will have to log out and log back in for this to take effect. You can also try
`newgrp dialout` to avoid the need to log out and log back in.

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

We assume all the commands are executed from this directory (the checkout of
the Jaguar repository).

Start by setting the `JAG_TOIT_REPO_PATH`. Typically, this would  be
the path to the third_party directory:
``` sh
export JAG_TOIT_REPO_PATH=$PWD/third_party/toit
```
Alternatively, `JAG_TOIT_REPO_PATH` could point to a different checkout of Toit.

Setup the ESP-IDF environment variables and PATHs, which will allow to compile
ESP32 programs. The easiest is to just use the `export.sh` that comes with
the ESP-IDF repository:
``` sh
source $JAG_TOIT_REPO_PATH/third_party/esp-idf/export.sh
```
Note that Toit's ESP-IDF is patched. Don't use use a plain ESP-IDF checkout instead.

Compile everything.
``` sh
make
```
This will build the SDK from the `JAG_TOIT_REPO_PATH`, then use it to download
the Toit dependencies (using `toit.pkg`) and finally build Jaguar; both the
host executable, as well as the Toit program that runs on the device.

You can now use Jaguar as usual:

``` sh
build/jag flash
sleep 3        # Give the device time to connect to the WiFi.
build/jag scan # Select the new device.
build/jag run $JAG_TOIT_REPO_PATH/examples/hello.toit
build/jag monitor
```

## Contributing
We welcome and value your [open source contributions](CONTRIBUTING.md) to Jaguar.
