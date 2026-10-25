package diskhistory

import (
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func skipEntry(info, root os.FileInfo) bool {
	if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return true
	}
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && data.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

type fileKey struct{}

// Windows reports a file's link count only through a handle, which the scan
// does not open, so each hard-link name counts.
func hardLink(os.FileInfo) (fileKey, bool) { return fileKey{}, false }

var getCompressedFileSize = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetCompressedFileSizeW")

// allocated asks for the bytes a sparse or compressed file stores; other files
// count their length. The call takes a path, so a disk image that another
// process holds open is measured without the scan opening it for reading.
func allocated(path string, info os.FileInfo) (int64, error) {
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok || data.FileAttributes&(windows.FILE_ATTRIBUTE_SPARSE_FILE|windows.FILE_ATTRIBUTE_COMPRESSED) == 0 {
		return info.Size(), nil
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var high uint32
	low, _, err := getCompressedFileSize.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&high)))
	if uint32(low) == 0xFFFFFFFF && err != windows.ERROR_SUCCESS {
		return 0, err
	}
	return int64(high)<<32 | int64(uint32(low)), nil
}
