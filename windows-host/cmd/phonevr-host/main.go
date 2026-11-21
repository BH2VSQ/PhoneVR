package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

var appVersion = "0.4.7"

func main() {
	listenAddress := flag.String("listen", "0.0.0.0", "IPv4 address to bind")
	discovery := flag.Int("discovery-port", discoveryPortDefault, "UDP discovery port")
	control := flag.Int("control-port", controlPortDefault, "TCP control port")
	pose := flag.Int("pose-port", posePortDefault, "UDP head-pose port")
	headless := flag.Bool("headless", false, "run the host service without opening the native desktop UI")
	ffmpegPath := flag.String("ffmpeg", "", "optional FFmpeg executable path for runtime video-encoder discovery")
	flag.Parse()

	service := NewHostService(*headless)
	config := HostConfig{
		ListenAddress: *listenAddress,
		DiscoveryPort: *discovery,
		ControlPort:   *control,
		PosePort:      *pose,
	}
	if err := service.Start(config); err != nil {
		service.setError(err.Error())
		if *headless {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	if driverMessage, driverErr := ensureBundledPhoneVRDriverAtStartup(); driverErr != nil {
		service.addLog("SteamVR driver auto-deploy: " + driverErr.Error())
	} else if driverMessage != "" {
		service.addLog(driverMessage)
	}

	service.ProbeVideoEncoders(*ffmpegPath)

	if *headless {
		fmt.Println("PhoneVR Host running in headless mode. Press Ctrl+C to stop.")
		for _, ip := range localIPv4Addresses() {
			fmt.Printf("LAN address: %s\n", ip)
		}
		signalChannel := make(chan os.Signal, 1)
		signal.Notify(signalChannel, os.Interrupt, syscall.SIGTERM)
		<-signalChannel
		service.Stop()
		return
	}

	if err := runNativeDesktopApp(service, config); err != nil {
		service.addLog("Native desktop UI stopped: " + err.Error())
	}
	service.Stop()
}
