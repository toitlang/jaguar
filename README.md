
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

## Contributing

We welcome and value your [open source contributions](CONTRIBUTING.md) to Jaguar (or Shaguar as we
like to call it).
