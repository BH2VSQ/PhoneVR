//go:build windows && amd64

package aoa

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	genericReadWrite               = 0xC0000000
	openExisting                   = 3
	shareReadWrite                 = 0x00000003
	fileAttributeNormal            = 0x00000080
	fileFlagOverlapped             = 0x40000000
	invalidHandle                  = ^uintptr(0)
	setupPresent                   = 0x00000002
	setupDeviceInterface           = 0x00000010
	registryKeyRead                = 0x00020019
	hkeyLocalMachine               = 0x80000002
	registryNoMoreItems            = 259
	registryString                 = 1
	registryMultiString            = 7
	tokenQuery                     = 0x0008
	tokenInformationClassElevation = 20
	usbDescriptorDevice            = 1
	usbPipeBulk                    = 2
	usbPipePolicyTransferTimeout   = 0x03
	aoaBulkOutTransferTimeoutMs    = 1500
	usbDirectionIn                 = 0x80
	usbVendorGoogle                = 0x18D1
	usbProductAccessory            = 0x2D00
	usbProductAccessoryADB         = 0x2D01
	usbRequestGetProtocol          = 51
	usbRequestSendString           = 52
	usbRequestStartAccessory       = 53
	aoaProbeRetryInterval          = 5 * time.Second
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
	procGetCurrentProcess               = kernel32.NewProc("GetCurrentProcess")
	procOpenProcessToken                = advapi32.NewProc("OpenProcessToken")
	procGetTokenInformation             = advapi32.NewProc("GetTokenInformation")
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
	procWinUsbSetPipePolicy             = winusb.NewProc("WinUsb_SetPipePolicy")
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
	writeMu       sync.Mutex
	loggerMu      sync.RWMutex
	logger        func(string)
	readLogCount  uint64
	writeLogCount uint64
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
					if report != nil {
						report("bulk-ready:"+path, fmt.Sprintf(
							"USB AOA Bulk endpoints ready for %04X:%04X: interface=0 IN=0x%02X OUT=0x%02X maxPacket=%d OUT-timeout=%dms",
							vid, pid, device.inPipe, device.outPipe, device.maxPacketSize, aoaBulkOutTransferTimeoutMs))
					}
					procSetupDiDestroyDeviceInfoList.Call(classPtr)
					return device, switched, nil
				} else if report != nil {
					report("bulk-endpoints:"+path, fmt.Sprintf(
						"USB AOA accessory %04X:%04X was detected, but its Bulk interface could not be opened: %v",
						vid, pid, err))
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

func windowsCallErrorCode(err error) (uint32, bool) {
	var code syscall.Errno
	if !errors.As(err, &code) {
		return 0, false
	}
	return uint32(code), true
}

func formatWindowsCallError(stage string, err error) error {
	if code, ok := windowsCallErrorCode(err); ok {
		return fmt.Errorf("%s failed (Win32 error %d): %w", stage, code, err)
	}
	return fmt.Errorf("%s failed: %w", stage, err)
}

type tokenElevationInfo struct {
	TokenIsElevated uint32
}

func hostProcessElevation() string {
	process, _, _ := procGetCurrentProcess.Call()
	var token uintptr
	opened, _, openErr := procOpenProcessToken.Call(process, tokenQuery, uintptr(unsafe.Pointer(&token)))
	if opened == 0 {
		return "unknown (OpenProcessToken: " + formatWindowsCallError("OpenProcessToken", openErr).Error() + ")"
	}
	defer procCloseHandle.Call(token)
	var elevation tokenElevationInfo
	var returned uint32
	queried, _, queryErr := procGetTokenInformation.Call(token, tokenInformationClassElevation,
		uintptr(unsafe.Pointer(&elevation)), uintptr(unsafe.Sizeof(elevation)), uintptr(unsafe.Pointer(&returned)))
	if queried == 0 {
		return "unknown (GetTokenInformation: " + formatWindowsCallError("GetTokenInformation", queryErr).Error() + ")"
	}
	if elevation.TokenIsElevated != 0 {
		return "true"
	}
	return "false"
}

func createWinUSBFile(path string, access uint32) (syscall.Handle, error) {
	wide, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return syscall.InvalidHandle, err
	}
	// WinUsb_Initialize requires FILE_FLAG_OVERLAPPED. Use Go's standard
	// Windows syscall wrapper for CreateFileW so its HANDLE and last-error
	// handling follow the same implementation used elsewhere in Go's stdlib.
	return syscall.CreateFile(wide, access, shareReadWrite, nil, openExisting,
		fileAttributeNormal|fileFlagOverlapped, 0)
}

func probeWinUSBAccess(path string, access uint32) string {
	handle, err := createWinUSBFile(path, access)
	if err != nil {
		if code, ok := windowsCallErrorCode(err); ok {
			return fmt.Sprintf("failed (Win32 error %d: %v)", code, err)
		}
		return "failed (" + err.Error() + ")"
	}
	_ = syscall.CloseHandle(handle)
	return "success"
}

func openWinUSB(path string) (*Device, error) {
	handle, err := createWinUSBFile(path, genericReadWrite)
	if err != nil {
		if code, ok := windowsCallErrorCode(err); ok && code == 5 {
			readOnly := probeWinUSBAccess(path, 0x80000000)
			writeOnly := probeWinUSBAccess(path, 0x40000000)
			return nil, fmt.Errorf("CreateFileW denied access (Win32 error 5, ERROR_ACCESS_DENIED; access=0x%08X share=0x%08X flags=0x%08X; Host PID=%d elevated=%s; read-only probe=%s; write-only probe=%s): %w",
				genericReadWrite, shareReadWrite, fileAttributeNormal|fileFlagOverlapped,
				os.Getpid(), hostProcessElevation(), readOnly, writeOnly, err)
		}
		return nil, formatWindowsCallError("CreateFileW", err)
	}
	var iface uintptr
	result, _, initErr := procWinUsbInitialize.Call(uintptr(handle), uintptr(unsafe.Pointer(&iface)))
	if result == 0 {
		_ = syscall.CloseHandle(handle)
		return nil, formatWindowsCallError("WinUsb_Initialize", initErr)
	}
	return &Device{handle: uintptr(handle), iface: iface, id: path}, nil
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
	stringsToSend := []string{"VSQ Studio", "PhoneVR", "PhoneVR USB streaming", "2.0.0", "https://github.com/BH2VSQ/PhoneVR", "PhoneVR-Android"}
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

func describeUSBInterface(descriptor usbInterfaceDescriptor) string {
	return fmt.Sprintf(
		"interface=%d alternate=%d endpoints=%d class=0x%02X subclass=0x%02X protocol=0x%02X",
		descriptor.InterfaceNumber, descriptor.AlternateSetting, descriptor.NumEndpoints,
		descriptor.InterfaceClass, descriptor.InterfaceSubClass, descriptor.InterfaceProtocol)
}

func validateAOAAccessoryInterface(descriptor usbInterfaceDescriptor) error {
	interfaceInfo := describeUSBInterface(descriptor)
	if descriptor.InterfaceNumber == 0 {
		return nil
	}
	if descriptor.InterfaceNumber == 1 && descriptor.InterfaceClass == 0xFF &&
		descriptor.InterfaceSubClass == 0x42 && descriptor.InterfaceProtocol == 0x01 {
		return fmt.Errorf("WinUSB is bound to the ADB interface (%s); AOA data uses interface 0, while interface 1 is ADB on product 18D1:2D01. Bind WinUSB to the Android Accessory interface (MI_00), not the ADB interface (MI_01)", interfaceInfo)
	}
	return fmt.Errorf("WinUSB default interface is not the AOA accessory interface (%s); AOA data requires interface 0. Check that WinUSB is bound to the accessory function rather than another interface", interfaceInfo)
}

func (d *Device) findBulkEndpoints() error {
	var descriptor usbInterfaceDescriptor
	result, _, callErr := procWinUsbQueryInterfaceSettings.Call(d.iface, 0, uintptr(unsafe.Pointer(&descriptor)))
	if result == 0 {
		return formatWindowsCallError("WinUsb_QueryInterfaceSettings", callErr)
	}
	if err := validateAOAAccessoryInterface(descriptor); err != nil {
		return err
	}
	var pipeInfo []string
	for index := byte(0); index < descriptor.NumEndpoints; index++ {
		var pipe winUSBPipeInformation
		result, _, pipeErr := procWinUsbQueryPipe.Call(d.iface, 0, uintptr(index), uintptr(unsafe.Pointer(&pipe)))
		if result == 0 {
			pipeInfo = append(pipeInfo, fmt.Sprintf("pipe[%d] query failed: %v", index, pipeErr))
			continue
		}
		pipeInfo = append(pipeInfo, fmt.Sprintf("pipe[%d]={type=%d id=0x%02X maxPacket=%d}",
			index, pipe.PipeType, pipe.PipeID, pipe.MaximumPacketSize))
		if pipe.PipeType != usbPipeBulk {
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
		return fmt.Errorf("AOA interface is missing Bulk IN or OUT endpoints (%s; discovered %s)",
			describeUSBInterface(descriptor), strings.Join(pipeInfo, ", "))
	}
	// Bulk OUT defaults to an infinite wait in WinUSB. Bound it so a stalled
	// phone receive endpoint cannot hold the shared PVRU writer indefinitely.
	if err := d.setPipeTransferTimeout(d.outPipe, aoaBulkOutTransferTimeoutMs); err != nil {
		return fmt.Errorf("configure AOA Bulk OUT transfer timeout: %w", err)
	}
	return nil
}

func (d *Device) setPipeTransferTimeout(pipeID byte, timeoutMs uint32) error {
	if d == nil || d.iface == 0 || pipeID == 0 || timeoutMs == 0 {
		return errors.New("invalid WinUSB pipe timeout configuration")
	}
	result, _, callErr := procWinUsbSetPipePolicy.Call(
		d.iface, uintptr(pipeID), uintptr(usbPipePolicyTransferTimeout),
		uintptr(unsafe.Sizeof(timeoutMs)), uintptr(unsafe.Pointer(&timeoutMs)),
	)
	if result == 0 {
		return formatWindowsCallError("WinUsb_SetPipePolicy(PIPE_TRANSFER_TIMEOUT)", callErr)
	}
	return nil
}

func (d *Device) SetLogger(logger func(string)) {
	d.loggerMu.Lock()
	d.logger = logger
	d.loggerMu.Unlock()
}

func (d *Device) logf(format string, args ...any) {
	d.loggerMu.RLock()
	logger := d.logger
	d.loggerMu.RUnlock()
	if logger != nil {
		logger(fmt.Sprintf(format, args...))
	}
}

func diagnosticHex(buffer []byte, limit int) string {
	if len(buffer) <= limit {
		return hex.EncodeToString(buffer)
	}
	half := limit / 2
	return hex.EncodeToString(buffer[:half]) + "..." + hex.EncodeToString(buffer[len(buffer)-half:])
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
			err := formatWindowsCallError("WinUsb_ReadPipe", callErr)
			d.logf("Bulk IN ReadPipe failed requested=%d transferred=%d error=%v", len(transfer), transferred, err)
			return 0, err
		}
		if transferred == 0 {
			return 0, nil
		}
		d.readLogCount++
		if d.readLogCount <= 12 {
			d.logf("Bulk IN ReadPipe #%d requested=%d transferred=%d crc32=%08X sampleHex=%s",
				d.readLogCount, len(transfer), transferred, crc32.ChecksumIEEE(transfer[:transferred]), diagnosticHex(transfer[:transferred], 32))
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
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	var transferred uint32
	result, _, callErr := procWinUsbWritePipe.Call(d.iface, uintptr(d.outPipe), uintptr(unsafe.Pointer(&buffer[0])), uintptr(uint32(len(buffer))), uintptr(unsafe.Pointer(&transferred)), 0)
	var err error
	if result == 0 {
		err = formatWindowsCallError("WinUsb_WritePipe", callErr)
	}
	d.writeLogCount++
	if d.writeLogCount <= 40 || err != nil || int(transferred) != len(buffer) {
		d.logf("Bulk OUT WritePipe #%d requested=%d transferred=%d pipe=0x%02X crc32=%08X sampleHex=%s error=%v",
			d.writeLogCount, len(buffer), transferred, d.outPipe, crc32.ChecksumIEEE(buffer), diagnosticHex(buffer, 32), err)
	}
	return int(transferred), err
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
	return pid == usbProductAccessory || pid == usbProductAccessoryADB
}
