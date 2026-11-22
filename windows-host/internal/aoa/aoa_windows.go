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
	registryKeyRead             = 0x00020019
	hkeyLocalMachine            = 0x80000002
	registryNoMoreItems         = 259
	registryString              = 1
	registryMultiString         = 7
	usbDescriptorDevice         = 1
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
	aoaProbeRetryInterval       = 5 * time.Second
)

var (
	setupapi = syscall.NewLazyDLL("setupapi.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	winusb   = syscall.NewLazyDLL("winusb.dll")

	procSetupDiGetClassDevs             = setupapi.NewProc("SetupDiGetClassDevsW")
	procSetupDiEnumDeviceInterfaces     = setupapi.NewProc("SetupDiEnumDeviceInterfaces")
	procSetupDiGetDeviceInterfaceDetail = setupapi.NewProc("SetupDiGetDeviceInterfaceDetailW")
	procSetupDiDestroyDeviceInfoList    = setupapi.NewProc("SetupDiDestroyDeviceInfoList")
	procCreateFile                      = kernel32.NewProc("CreateFileW")
	procCloseHandle                     = kernel32.NewProc("CloseHandle")
	procRegOpenKeyEx                    = advapi32.NewProc("RegOpenKeyExW")
	procRegEnumKeyEx                    = advapi32.NewProc("RegEnumKeyExW")
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

type deviceInterfaceData struct {
	CbSize             uint32
	InterfaceClassGUID guid
	Flags              uint32
	Reserved           uintptr
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
	lastProbe := make(map[string]time.Time)
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
		device, _, err := scanOnce(emit, lastProbe)
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

func scanOnce(report func(key, message string), lastProbe map[string]time.Time) (*Device, bool, error) {
	switched := false
	seenPaths := make(map[string]struct{})
	interfaceGUIDs := enumerateWinUSBInterfaceGUIDs()
	if report != nil {
		report("guid-count", fmt.Sprintf("USB AOA scanner: enumerating %d registered WinUSB interface GUID(s)", len(interfaceGUIDs)))
		for _, interfaceGUID := range interfaceGUIDs {
			report("guid:"+formatGUID(interfaceGUID), "USB AOA scanner: registered WinUSB interface GUID "+formatGUID(interfaceGUID))
		}
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
			if lastProbe != nil {
				if retryAt, ok := lastProbe[path]; ok && time.Now().Before(retryAt) {
					continue
				}
			}
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
			if report != nil {
				report("device:"+path, fmt.Sprintf("USB AOA scanner: WinUSB USB device %04X:%04X (bDeviceClass=0x%02X)", vid, pid, class))
			}
			if vid == usbVendorGoogle && isAccessoryProduct(pid) {
				if err := device.findBulkEndpoints(); err == nil {
					procSetupDiDestroyDeviceInfoList.Call(classPtr)
					return device, switched, nil
				}
				device.Close()
				continue
			}
			// The AOA specification does not define an Android OEM VID allow-list.
			// It requires the accessory to issue GET_PROTOCOL when the descriptor
			// does not already identify an AOA accessory. Probe each present
			// WinUSB-backed USB function, then accept only the defined AOA v1/v2
			// protocol numbers before sending any mode-switch strings.
			if lastProbe != nil {
				lastProbe[path] = time.Now().Add(aoaProbeRetryInterval)
			}
			if report != nil {
				report("candidate:"+path, fmt.Sprintf("USB AOA scanner: probing GET_PROTOCOL on WinUSB device %04X:%04X (device-level endpoint 0)", vid, pid))
			}
			protocol, getErr := device.getAOAProtocol()
			if getErr != nil {
				if report != nil {
					report("get-protocol:"+path, fmt.Sprintf("USB AOA GET_PROTOCOL did not respond on %04X:%04X: %v", vid, pid, getErr))
				}
			} else if !isSupportedAOAProtocol(protocol) {
				if report != nil {
					report("protocol-version:"+path, fmt.Sprintf("USB AOA returned unsupported protocol version %d on %04X:%04X; expected v1 or v2", protocol, vid, pid))
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

// enumerateWinUSBInterfaceGUIDs reads interface GUIDs registered by installed
// WinUSB functions from HKLM\SYSTEM\CurrentControlSet\Enum\USB. GUIDs are
// generated by the device INF or Windows USB descriptors and can differ by
// OEM, driver package, PC and USB instance. No PhoneVR-specific or Android-OEM
// GUID is assumed. The generic GUID_DEVINTERFACE_USB_DEVICE is deliberately
// excluded because it identifies a USB devnode, not a WinUSB function handle.
func enumerateWinUSBInterfaceGUIDs() []guid {
	rootPath, _ := syscall.UTF16PtrFromString("SYSTEM\\CurrentControlSet\\Enum\\USB")
	var rootKey uintptr
	status, _, _ := procRegOpenKeyEx.Call(hkeyLocalMachine, uintptr(unsafe.Pointer(rootPath)), 0,
		registryKeyRead, uintptr(unsafe.Pointer(&rootKey)))
	if status != 0 || rootKey == 0 {
		return nil
	}
	defer procRegCloseKey.Call(rootKey)

	var registryValues [][]string
	for _, vendorProduct := range enumerateRegistrySubkeys(rootKey) {
		vendorPath, _ := syscall.UTF16PtrFromString(vendorProduct)
		var vendorKey uintptr
		status, _, _ = procRegOpenKeyEx.Call(rootKey, uintptr(unsafe.Pointer(vendorPath)), 0,
			registryKeyRead, uintptr(unsafe.Pointer(&vendorKey)))
		if status != 0 || vendorKey == 0 {
			continue
		}
		for _, instance := range enumerateRegistrySubkeys(vendorKey) {
			instancePath, _ := syscall.UTF16PtrFromString(instance)
			var instanceKey uintptr
			status, _, _ = procRegOpenKeyEx.Call(vendorKey, uintptr(unsafe.Pointer(instancePath)), 0,
				registryKeyRead, uintptr(unsafe.Pointer(&instanceKey)))
			if status != 0 || instanceKey == 0 {
				continue
			}
			// Only collect GUIDs from device instances whose function driver is
			// explicitly registered as WinUSB. This avoids opening unrelated USB
			// interfaces that happen to have other device-interface GUIDs.
			services := readRegistryStrings(instanceKey, "Service")
			service := ""
			if len(services) > 0 {
				service = services[0]
			}
			if !includeWinUSBService(service) {
				procRegCloseKey.Call(instanceKey)
				continue
			}
			parametersPath, _ := syscall.UTF16PtrFromString("Device Parameters")
			var parametersKey uintptr
			status, _, _ = procRegOpenKeyEx.Call(instanceKey, uintptr(unsafe.Pointer(parametersPath)), 0,
				registryKeyRead, uintptr(unsafe.Pointer(&parametersKey)))
			if status == 0 && parametersKey != 0 {
				values := append(readRegistryGUIDValue(parametersKey, "DeviceInterfaceGUIDs"),
					readRegistryGUIDValue(parametersKey, "DeviceInterfaceGUID")...)
				procRegCloseKey.Call(parametersKey)
				if len(values) > 0 {
					registryValues = append(registryValues, values)
				}
			}
			procRegCloseKey.Call(instanceKey)
		}
		procRegCloseKey.Call(vendorKey)
	}
	return parseUniqueGUIDs(registryValues...)
}

func enumerateRegistrySubkeys(key uintptr) []string {
	var names []string
	for index := uint32(0); ; index++ {
		buffer := make([]uint16, 512)
		nameLength := uint32(len(buffer))
		status, _, _ := procRegEnumKeyEx.Call(key, uintptr(index),
			uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&nameLength)),
			0, 0, 0, 0)
		if status == registryNoMoreItems {
			break
		}
		if status != 0 {
			break
		}
		if nameLength > 0 && nameLength <= uint32(len(buffer)) {
			names = append(names, syscall.UTF16ToString(buffer[:nameLength]))
		}
	}
	return names
}

func readRegistryGUIDValue(key uintptr, name string) []string {
	return readRegistryStrings(key, name)
}

func readRegistryStrings(key uintptr, name string) []string {
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
	stringsToSend := []string{"VSQ Studio", "PhoneVR", "PhoneVR USB streaming", "0.5.2", "https://github.com/BH2VSQ/PhoneVR", "PhoneVR-Android"}
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
