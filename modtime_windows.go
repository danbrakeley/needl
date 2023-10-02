package main

import (
	"syscall"
	"time"
)

func modifyFileTime(path string, stamp time.Time) error {
	pathp, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}

	h, err := syscall.CreateFile(pathp,
		syscall.FILE_WRITE_ATTRIBUTES, syscall.FILE_SHARE_WRITE, nil,
		syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS, 0,
	)
	if err != nil {
		return err
	}
	defer syscall.Close(h)

	ft := syscall.NsecToFiletime(stamp.UnixNano())
	return syscall.SetFileTime(h, nil, nil, &ft)
}
