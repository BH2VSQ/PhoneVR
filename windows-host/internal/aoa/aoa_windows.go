//go:build windows && amd64

package aoa

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	genericReadWrite            = 0xC0000000
	openExisting                = 3
	shareReadWrite              = 0x00000003
	invalidHandle               = ^uintptr(0)
	setupPresent                = 0x00000002
	setupDeviceInterface        = 0x00000010
	deviceRegistryScopeGlobal   = 0x00000001
	deviceRegistryKey           = 0x00000001
	registryKeyRead             = 0x00020019
	registryString              = 1
	registryMultiString         = 7
	usbDescriptorDevice         = 1
	usbDescriptorTypeInterface  = 4
	usbPipeBulk                 = 2
	usbDirectionIn              = 0x80
	usbVendorGoogle             = 0x18D1
	usbProductAccessory         = 0x2D00
	usbProductAccessoryADB      = 0x2D01
	usbProductAccessoryAudio    = 0x2D04
	usbProductAccessoryAudioADB = 0x2D05
	usbRequestGetProtocol       = 51
	usbRequestSendString        = 52
	usbRequestStartAccessory    = 53
)

var (
	setupapi = syscall.NewLazyDLL("setupapi.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	winusb   = syscall.NewLazyDLL("winusb.dll")

	procSetupDiGetClassDevs             = setupapi.NewProc("SetupDiGetClassDevsW")
	procSetupDiEnumDeviceInterfaces     = setupapi.NewProc("SetupDiEnumDeviceInterfaces")
	procSetupDiEnumDeviceInfo           = setupapi.NewProc("SetupDiEnumDeviceInfo")
	procSetupDiOpenDevRegKey            = setupapi.NewProc("SetupDiOpenDevRegKey")
	procSetupDiGetDeviceInterfaceDetail = setupapi.NewProc("SetupDiGetDeviceInterfaceDetailW")
	procSetupDiDestroyDeviceInfoList    = setupapi.NewProc("SetupDiDestroyDeviceInfoList")
	procCreateFile                      = kernel32.NewProc("CreateFileW")
	procCloseHandle                     = kernel32.NewProc("CloseHandle")
	procRegOpenKeyEx                    = advapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueEx                 = advapi32.NewProc("RegQueryValueExW")
	procRegCloseKey                     = advapi32.NewProc("RegCloseKey")
	procWinUsbInitialize                = winusb.NewProc("WinUsb_Initialize")
	procWinUsbFree                      = winusb.NewProc("WinUsb_Free")
	procWinUsbControlTransfer           = winusb.NewProc("WinUsb_ControlTransfer")
	procWinUsbGetDescriptor             = winusb.NewProc("WinUsb_GetDescriptor")
	procWinUsbQueryInterfaceSettings    = winusb.NewProc("WinUsb_QueryInterfaceSettings")
	procWinUsbQueryPipe                 = winusb.NewProc("WinUsb_QueryPipe")
	procWinUsbReadPipe                  = winusb.NewProc("WinUsb_ReadPipe")
	procWinUsbWritePipe                 = winusb.NewProc("WinUsb_WritePipe")
	procWinUsbAbortPipe                 = winusb.NewProc("WinUsb_AbortPipe")
)

// GUID_DEVINTERFACE_USB_DEVICE, which SetupAPI exposes for USB devices whose
// function driver supports a user-mode interface such as WinUSB.
var usbDeviceInterfaceGUID = guid{0xA5DCBF10, 0x6530, 0x11D2, [8]byte{0x90, 0x1F, 0x00, 0xC0, 0x4F, 0xB9, 0x51, 0xED}}

type deviceInterfaceData struct {
	CbSize             uint32
	InterfaceClassGUID guid
	Flags              uint32
	Reserved           uintptr
}
type deviceInfoData struct {
	CbSize    uint32
	ClassGUID guid
	DevInst   uint32
	Reserved  uintptr
}
type usbInterfaceDescriptor struct {
	Length            byte
	DescriptorType    byte
	InterfaceNumber   byte
	AlternateSetting  byte
	NumEndpoints      byte
	InterfaceClass    byte
	InterfaceSubClass byte
	InterfaceProtocol byte
	InterfaceIndex    byte
}
type winUSBPipeInformation struct {
	PipeType          uint32
	PipeID            byte
	MaximumPacketSize uint16
	Interval          byte
}

type Device struct {
	handle        uintptr
	iface         uintptr
	inPipe        byte
	outPipe       byte
	maxPacketSize uint16
	readMu        sync.Mutex
	readPending   []byte
	id            string
	closeOnce     sync.Once
}

func NextAccessory(ctx context.Context) (*Device, error) {
	return NextAccessoryWithStatus(ctx, nil)
}

// NextAccessoryWithStatus scans for an AOA device and reports throttled
// diagnostics. AOA failures are otherwise easy to mistake for a silent wait
// on the Android side because the phone cannot enter accessory mode until
// the host completes the endpoint-zero control handshake.
func NextAccessoryWithStatus(ctx context.Context, report func(string)) (*Device, error) {
	lastReport := make(map[string]time.Time)
	emit := func(key, message string) {
		if report == nil {
			return
		}
		now := time.Now()
		if previous, ok := lastReport[key]; ok && now.Sub(previous) < 15*time.Second {
			return
		}
		lastReport[key] = now
		report(message)
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		device, _, err := scanOnce(emit)
		if device != nil {
			return device, nil
		}
		if err != nil {
			emit("scan-error:"+err.Error(), "USB AOA scanner error: "+err.Error())
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(900 * time.Millisecond):
		}
	}
}

func scanOnce(report func(key, message string)) (*Device, bool, error) {
	switched := false
	seenPaths := make(map[string]struct{})
	interfaceGUIDs := enumerateWinUSBInterfaceGUIDs()
	if report != nil {
		report("guid-count", fmt.Sprintf("USB AOA scanner: enumerating %d WinUSB interface GUID(s)", len(interfaceGUIDs)))
	}
	seenCount := 0
	for _, interfaceGUID := range interfaceGUIDs {
		classPtr, _, _ := procSetupDiGetClassDevs.Call(
			uintptr(unsafe.Pointer(&interfaceGUID)), 0, 0,
			setupPresent|setupDeviceInterface,
		)
		if classPtr == invalidHandle {
			if report != nil {
				report("setupapi-class:"+formatGUID(interfaceGUID), fmt.Sprintf("USB AOA scanner: SetupDiGetClassDevs failed for interface GUID %s", formatGUID(interfaceGUID)))
			}
			continue
		}
		for index := uint32(0); ; index++ {
			var data deviceInterfaceData
			data.CbSize = uint32(unsafe.Sizeof(data))
			result, _, enumErr := procSetupDiEnumDeviceInterfaces.Call(classPtr, 0,
				uintptr(unsafe.Pointer(&interfaceGUID)), uintptr(index), uintptr(unsafe.Pointer(&data)))
			if result == 0 {
				if errno, ok := enumErr.(syscall.Errno); ok && errno == 259 {
					break
				}
				break
			}
			path, err := interfacePath(classPtr, &data)
			if err != nil {
				if report != nil {
					report("interface-path:"+formatGUID(interfaceGUID), "USB AOA scanner: unable to read a WinUSB device interface path: "+err.Error())
				}
				continue
			}
			if _, duplicate := seenPaths[path]; duplicate {
				continue
			}
			seenPaths[path] = struct{}{}
			seenCount++
			device, err := openWinUSB(path)
			if err != nil {
				if report != nil {
					report("open:"+path, fmt.Sprintf("USB AOA scanner: WinUSB open failed for %s: %v", path, err))
				}
				continue
			}
			vid, pid, class, err := device.deviceDescriptor()
			if err != nil {
				if report != nil {
					report("descriptor:"+path, fmt.Sprintf("USB AOA scanner: device descriptor read failed for %s: %v", path, err))
				}
				device.Close()
				continue
			}
			if isLikelyAndroidVendor(vid) && report != nil {
				report("device:"+path, fmt.Sprintf("USB AOA scanner: Android USB device %04X:%04X (bDeviceClass=0x%02X)", vid, pid, class))
			}
			if vid == usbVendorGoogle && isAccessoryProduct(pid) {
				if err := device.findBulkEndpoints(); err == nil {
					procSetupDiDestroyDeviceInfoList.Call(classPtr)
					return device, switched, nil
				}
				device.Close()
				continue
			}
			// Android devices can expose a vendor-specific ADB interface class
			// (0xFF/0x42/0x01), as on this user's 18D1:4E11 device. AOA control
			// requests still target device endpoint zero; no ADB bulk endpoints
			// or ADB protocol messages are used by PhoneVR.
			if isLikelyAndroidVendor(vid) {
				if report != nil {
					report("candidate:"+path, fmt.Sprintf("USB AOA scanner: probing AOA GET_PROTOCOL on %04X:%04X (WinUSB path: %s)", vid, pid, path))
				}
				protocol, getErr := device.getAOAProtocol()
				if getErr != nil {
					if report != nil {
						report("get-protocol:"+path, fmt.Sprintf("USB AOA GET_PROTOCOL failed for %04X:%04X: %v", vid, pid, getErr))
					}
				} else if protocol < 1 {
					if report != nil {
						report("protocol-version:"+path, fmt.Sprintf("USB AOA protocol version %d is unsupported on %04X:%04X", protocol, vid, pid))
					}
				} else {
					if report != nil {
						report("protocol-ok:"+path, fmt.Sprintf("USB AOA GET_PROTOCOL succeeded for %04X:%04X (protocol v%d)", vid, pid, protocol))
					}
					if startErr := device.startAccessoryMode(); startErr == nil {
						switched = true
						if report != nil {
							report("start-request:"+path, fmt.Sprintf("USB AOA START_ACCESSORY accepted for %04X:%04X; waiting for USB re-enumeration", vid, pid))
						}
					} else {
						if report != nil {
							report("start-failed:"+path, fmt.Sprintf("USB AOA START_ACCESSORY failed for %04X:%04X: %v", vid, pid, startErr))
						}
					}
				}
			}
			device.Close()
		}
		procSetupDiDestroyDeviceInfoList.Call(classPtr)
	}
	if report != nil {
		report("path-count", fmt.Sprintf("USB AOA scanner: enumerated %d present WinUSB device interface path(s)", seenCount))
		if seenCount == 0 {
			report("no-winusb-paths", "USB AOA scanner: no present WinUSB device interface paths found; check DeviceInterfaceGUID / DeviceInterfaceGUIDs registration")
		}
	}
	return nil, switched, nil
}

// enumerateWinUSBInterfaceGUIDs reads the interface GUIDs registered by the
// WinUSB function driver. Tools such as Zadig may generate a unique GUID per
// device, so relying only on GUID_DEVINTERFACE_USB_DEVICE would miss them.
func enumerateWinUSBInterfaceGUIDs() []guid {
	result := []guid{usbDeviceInterfaceGUID}
	seen := map[guid]struct{}{usbDeviceInterfaceGUID: {}}
	enumerator, _ := syscall.UTF16PtrFromString("USB")
	infoSet, _, _ := procSetupDiGetClassDevs.Call(0, uintptr(unsafe.Pointer(enumerator)), 0, setupPresent)
	if infoSet == invalidHandle {
		return result
	}
	defer procSetupDiDestroyDeviceInfoList.Call(infoSet)
	for index := uint32(0); ; index++ {
		var info deviceInfoData
		info.CbSize = uint32(unsafe.Sizeof(info))
		ok, _, enumErr := procSetupDiEnumDeviceInfo.Call(infoSet, uintptr(index), uintptr(unsafe.Pointer(&info)))
		if ok == 0 {
			if errno, isErrno := enumErr.(syscall.Errno); isErrno && errno == 259 {
				break
			}
			break
		}
		for _, value := range readDeviceInterfaceGUIDs(infoSet, &info) {
			parsed, err := parseGUID(value)
			if err != nil {
				continue
			}
			if _, exists := seen[parsed]; exists {
				continue
			}
			seen[parsed] = struct{}{}
			result = append(result, parsed)
		}
	}
	return result
}

func readDeviceInterfaceGUIDs(infoSet uintptr, info *deviceInfoData) []string {
	deviceKey, _, _ := procSetupDiOpenDevRegKey.Call(infoSet, uintptr(unsafe.Pointer(info)),
		deviceRegistryScopeGlobal, 0, deviceRegistryKey, registryKeyRead)
	if deviceKey == 0 || deviceKey == invalidHandle {
		return nil
	}
	defer procRegCloseKey.Call(deviceKey)
	parameters, _ := syscall.UTF16PtrFromString("Device Parameters")
	var parameterKey uintptr
	status, _, _ := procRegOpenKeyEx.Call(deviceKey, uintptr(unsafe.Pointer(parameters)), 0, registryKeyRead, uintptr(unsafe.Pointer(&parameterKey)))
	if status != 0 {
		return nil
	}
	defer procRegCloseKey.Call(parameterKey)

	// Microsoft permits either a REG_SZ DeviceInterfaceGUID or a REG_MULTI_SZ
	// DeviceInterfaceGUIDs value. Zadig and vendor INF files do not all use
	// the same form, so support both instead of silently missing valid devices.
	values := append(readRegistryGUIDValue(parameterKey, "DeviceInterfaceGUIDs"), readRegistryGUIDValue(parameterKey, "DeviceInterfaceGUID")...)
	return values
}

func readRegistryGUIDValue(key uintptr, name string) []string {
	valueName, _ := syscall.UTF16PtrFromString(name)
	var valueType uint32
	var size uint32
	status, _, _ := procRegQueryValueEx.Call(key, uintptr(unsafe.Pointer(valueName)), 0, uintptr(unsafe.Pointer(&valueType)), 0, uintptr(unsafe.Pointer(&size)))
	if status != 0 || (valueType != registryString && valueType != registryMultiString) || size < 2 || size > 64*1024 || size%2 != 0 {
		return nil
	}
	buffer := make([]byte, size)
	status, _, _ = procRegQueryValueEx.Call(key, uintptr(unsafe.Pointer(valueName)), 0, uintptr(unsafe.Pointer(&valueType)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)))
	if status != 0 || size < 2 {
		return nil
	}
	units := unsafe.Slice((*uint16)(unsafe.Pointer(&buffer[0])), int(size/2))
	if valueType == registryString {
		value := strings.TrimSpace(syscall.UTF16ToString(units))
		if value == "" {
			return nil
		}
		return []string{value}
	}
	var values []string
	for offset := 0; offset < len(units); {
		end := offset
		for end < len(units) && units[end] != 0 {
			end++
		}
		if end == offset {
			break
		}
		values = append(values, strings.TrimSpace(syscall.UTF16ToString(units[offset:end])))
		offset = end + 1
	}
	return values
}

func formatGUID(value guid) string {
	return fmt.Sprintf("{%08X-%04X-%04X-%02X%02X-%02X%02X%02X%02X%02X%02X}", value.Data1, value.Data2, value.Data3,
		value.Data4[0], value.Data4[1], value.Data4[2], value.Data4[3], value.Data4[4], value.Data4[5], value.Data4[6], value.Data4[7])
}

func interfacePath(infoSet uintptr, data *deviceInterfaceData) (string, error) {
	var required uint32
	procSetupDiGetDeviceInterfaceDetail.Call(infoSet, uintptr(unsafe.Pointer(data)), 0, 0, uintptr(unsafe.Pointer(&required)), 0)
	if required < 8 || required > 64*1024 {
		return "", errors.New("invalid USB device interface path size")
	}
	buffer := make([]byte, required)
	binary.LittleEndian.PutUint32(buffer[:4], 8) // SP_DEVICE_INTERFACE_DETAIL_DATA_W cbSize on 64-bit Windows.
	result, _, callErr := procSetupDiGetDeviceInterfaceDetail.Call(infoSet, uintptr(unsafe.Pointer(data)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(required), uintptr(unsafe.Pointer(&required)), 0)
	if result == 0 {
		return "", fmt.Errorf("SetupDiGetDeviceInterfaceDetailW: %w", callErr)
	}
	pathUnits := (len(buffer) - 4) / 2
	path := syscall.UTF16ToString((*[1 << 15]uint16)(unsafe.Pointer(&buffer[4]))[:pathUnits])
	return path, nil
}

func openWinUSB(path string) (*Device, error) {
	wide, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, _, callErr := procCreateFile.Call(uintptr(unsafe.Pointer(wide)), genericReadWrite,
		shareReadWrite, 0, openExisting, 0, 0)
	if handle == invalidHandle {
		return nil, callErr
	}
	var iface uintptr
	result, _, initErr := procWinUsbInitialize.Call(handle, uintptr(unsafe.Pointer(&iface)))
	if result == 0 {
		procCloseHandle.Call(handle)
		return nil, initErr
	}
	return &Device{handle: handle, iface: iface, id: path}, nil
}

func (d *Device) deviceDescriptor() (uint16, uint16, byte, error) {
	buffer := make([]byte, 18)
	var transferred uint32
	result, _, callErr := procWinUsbGetDescriptor.Call(d.iface, usbDescriptorDevice, 0, 0,
		uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), uintptr(unsafe.Pointer(&transferred)))
	if result == 0 || transferred < 18 {
		return 0, 0, 0, fmt.Errorf("WinUsb_GetDescriptor: %w", callErr)
	}
	return binary.LittleEndian.Uint16(buffer[8:10]), binary.LittleEndian.Uint16(buffer[10:12]), buffer[4], nil
}

func (d *Device) getAOAProtocol() (uint16, error) {
	buffer := make([]byte, 2)
	transferred, err := d.controlTransfer(0xC0, usbRequestGetProtocol, 0, 0, buffer)
	if err != nil {
		return 0, err
	}
	if transferred != 2 {
		return 0, errors.New("AOA GET_PROTOCOL returned an incomplete response")
	}
	return binary.LittleEndian.Uint16(buffer), nil
}

func (d *Device) startAccessoryMode() error {
	stringsToSend := []string{"VSQ Studio", "PhoneVR", "PhoneVR USB streaming", "0.5.1", "https://github.com/BH2VSQ/PhoneVR", "PhoneVR-Android"}
	for index, value := range stringsToSend {
		payload := append([]byte(value), 0)
		transferred, err := d.controlTransfer(0x40, usbRequestSendString, 0, uint16(index), payload)
		if err != nil {
			return fmt.Errorf("AOA SEND_STRING[%d]: %w", index, err)
		}
		if transferred != uint32(len(payload)) {
			return fmt.Errorf("AOA SEND_STRING[%d] transferred %d of %d bytes", index, transferred, len(payload))
		}
	}
	if _, err := d.controlTransfer(0x40, usbRequestStartAccessory, 0, 0, nil); err != nil {
		return fmt.Errorf("AOA START_ACCESSORY: %w", err)
	}
	return nil
}

func (d *Device) controlTransfer(requestType, request byte, value, index uint16, payload []byte) (uint32, error) {
	setup := uintptr(packSetupPacket(requestType, request, value, index, uint16(len(payload))))
	var buffer uintptr
	if len(payload) > 0 {
		buffer = uintptr(unsafe.Pointer(&payload[0]))
	}
	var transferred uint32
	// WINUSB_SETUP_PACKET is an 8-byte structure passed by value by the
	// WinUSB ABI. Pack its little-endian fields into the second argument.
	result, _, callErr := procWinUsbControlTransfer.Call(d.iface, setup, buffer,
		uintptr(len(payload)), uintptr(unsafe.Pointer(&transferred)), 0)
	if result == 0 {
		return transferred, callErr
	}
	return transferred, nil
}

func (d *Device) findBulkEndpoints() error {
	var descriptor usbInterfaceDescriptor
	result, _, callErr := procWinUsbQueryInterfaceSettings.Call(d.iface, 0, uintptr(unsafe.Pointer(&descriptor)))
	if result == 0 {
		return fmt.Errorf("WinUsb_QueryInterfaceSettings: %w", callErr)
	}
	if descriptor.InterfaceNumber != 0 {
		return fmt.Errorf("AOA accessory interface number is %d, expected interface 0", descriptor.InterfaceNumber)
	}
	for index := byte(0); index < descriptor.NumEndpoints; index++ {
		var pipe winUSBPipeInformation
		result, _, _ := procWinUsbQueryPipe.Call(d.iface, 0, uintptr(index), uintptr(unsafe.Pointer(&pipe)))
		if result == 0 || pipe.PipeType != usbPipeBulk {
			continue
		}
		if pipe.MaximumPacketSize > d.maxPacketSize {
			d.maxPacketSize = pipe.MaximumPacketSize
		}
		if pipe.PipeID&usbDirectionIn != 0 {
			d.inPipe = pipe.PipeID
		} else {
			d.outPipe = pipe.PipeID
		}
	}
	if d.inPipe == 0 || d.outPipe == 0 {
		return errors.New("AOA interface does not expose both bulk IN and OUT endpoints")
	}
	return nil
}

func (d *Device) Read(buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}
	d.readMu.Lock()
	defer d.readMu.Unlock()
	if len(d.readPending) == 0 {
		packetSize := int(d.maxPacketSize)
		if packetSize <= 0 {
			packetSize = 512
		}
		readSize := (64 * 1024 / packetSize) * packetSize
		if readSize < packetSize {
			readSize = packetSize
		}
		transfer := make([]byte, readSize)
		var transferred uint32
		result, _, callErr := procWinUsbReadPipe.Call(d.iface, uintptr(d.inPipe), uintptr(unsafe.Pointer(&transfer[0])), uintptr(uint32(len(transfer))), uintptr(unsafe.Pointer(&transferred)), 0)
		if result == 0 {
			return 0, callErr
		}
		if transferred == 0 {
			return 0, nil
		}
		d.readPending = transfer[:transferred]
	}
	n := copy(buffer, d.readPending)
	d.readPending = d.readPending[n:]
	return n, nil
}

func (d *Device) Write(buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}
	var transferred uint32
	result, _, callErr := procWinUsbWritePipe.Call(d.iface, uintptr(d.outPipe), uintptr(unsafe.Pointer(&buffer[0])), uintptr(uint32(len(buffer))), uintptr(unsafe.Pointer(&transferred)), 0)
	if result == 0 {
		return int(transferred), callErr
	}
	return int(transferred), nil
}

func (d *Device) ID() string { return strings.TrimSpace(d.id) }

func (d *Device) Close() error {
	d.closeOnce.Do(func() {
		if d.iface != 0 {
			if d.inPipe != 0 {
				procWinUsbAbortPipe.Call(d.iface, uintptr(d.inPipe))
			}
			if d.outPipe != 0 {
				procWinUsbAbortPipe.Call(d.iface, uintptr(d.outPipe))
			}
			procWinUsbFree.Call(d.iface)
		}
		if d.handle != 0 && d.handle != invalidHandle {
			procCloseHandle.Call(d.handle)
		}
	})
	return nil
}

func isAccessoryProduct(pid uint16) bool {
	return pid == usbProductAccessory || pid == usbProductAccessoryADB || pid == usbProductAccessoryAudio || pid == usbProductAccessoryAudioADB
}

func isLikelyAndroidVendor(vid uint16) bool {
	switch vid {
	case 0x2717, 0x18D1, 0x04E8, 0x22B8, 0x0FCE, 0x2A70, 0x12D1, 0x19D2, 0x2A45, 0x17EF, 0x2C7C:
		return true
	default:
		return false
	}
}
