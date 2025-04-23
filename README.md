# Controlbox Simulator with HEMS Example

![ControlBoxSimulator-ezgif com-video-to-gif-converter](https://github.com/user-attachments/assets/2e48304f-3590-4a52-b349-7c2fab7876c8)

## Foreword

This project is based on the EEBUS stack developed by Andreas Linde, [eebus-go](https://github.com/enbility/eebus-go). Therefore, it is highly recommended to first familiarize yourself with the details of his project and especially read its [README](https://github.com/enbility/eebus-go/blob/dev/README.md) file.

The differences from the original repository include a modified `hems` example and an extended `controlbox` example, which has been enhanced with a web frontend similar to the approach taken in the [devices](https://github.com/enbility/devices) example. For testing purposes, the combination of the HEMS and controlbox simulators is ideal. In practice, the simulator has already been tested with the Vaillant Internet module VR921 and the EV Charge Controller [evcc](https://github.com/evcc-io/evcc). Additionally, there is a [folk](https://github.com/vollautomat/evcc) of evcc where the web frontend has been extended to display the currently applicable consumption limitation.

Additionally, the use cases from the [eebus-go](https://github.com/enbility/eebus-go) repository have been expanded to include basic implementations of the MPC and MGCP use cases for the actors Monitored Unit and Grid Connection Point.

## Installation

In contrast to the installation instructions in the enbility repository, the following should be noted for the controlbox simulator: The code is divided into two files, `main.go` and `frontend.go`. Therefore, it cannot be started in the usual way with `go run main.go …`. Instead of specifying a Go file, the directory containing both Go files must be used. Furthermore, when launching the controlbox simulator, the specification of the SKI parameter of the counterpart is omitted, as the simulator searches for all available EEBUS devices and offers a selection for manual connection.

The easiest way to start both HEMS and the controlbox simulator is to open the project in Visual Studio Code and create a `.vscode` directory in the root directory. Inside `.vscode`, create a file named `launch.json`, with content similar to the following. The SKI parameter for `hems` must, of course, be replaced with the respective one of the controlbox simulator, which can be easily read from its web frontend.

```{
    // Verwendet IntelliSense zum Ermitteln möglicher Attribute.
    // Zeigen Sie auf vorhandene Attribute, um die zugehörigen Beschreibungen anzuzeigen.
    // Weitere Informationen finden Sie unter https://go.microsoft.com/fwlink/?linkid=830387
    "version": "0.2.0",
    "configurations": [
        

        {
            "name": "Launch HEMS",
            "type": "go",
            "request": "launch",
            "mode": "auto",
            "program": "examples/hems/main.go",
            "env": {},
            "args": ["4711", "9d15806e0dea56ae0406328fa712ec6d4404a927", "./crt", "./key"]
        },
        {
            "name": "Launch Control Box",
            "type": "go",
            "request": "launch",
            "mode": "auto",
            "program": "examples/controlbox",
            "env": {},
            "args": ["4712", "./crt", "./key"]
        }
    ]
}
```
